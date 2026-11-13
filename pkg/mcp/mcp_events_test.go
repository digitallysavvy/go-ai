package mcp

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var testEventDefinition = MCPEventDefinition{
	Name:          "comment.created",
	Delivery:      []string{"webhook"},
	InputSchema:   map[string]interface{}{"type": "object"},
	PayloadSchema: map[string]interface{}{"type": "object"},
}

type memEventStore struct {
	mu      sync.Mutex
	records map[string]MCPEventSubscription
	failUpd error
	failDel error
}

func newMemEventStore() *memEventStore {
	return &memEventStore{records: map[string]MCPEventSubscription{}}
}

func (s *memEventStore) Get(_ context.Context, key string) (*MCPEventSubscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.records[key]
	if !ok {
		return nil, nil
	}
	return &v, nil
}

func (s *memEventStore) GetByID(_ context.Context, id string) (*MCPEventSubscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.records {
		if v.ID == id {
			v := v
			return &v, nil
		}
	}
	return nil, nil
}

func (s *memEventStore) Set(_ context.Context, sub MCPEventSubscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[sub.Key] = sub
	return nil
}

func (s *memEventStore) Update(_ context.Context, key string, p MCPEventSubscriptionPatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failUpd != nil {
		return s.failUpd
	}
	v, ok := s.records[key]
	if !ok {
		return errors.New("unknown subscription")
	}
	p.ApplyTo(&v)
	s.records[key] = v
	return nil
}

func (s *memEventStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failDel != nil {
		return s.failDel
	}
	delete(s.records, key)
	return nil
}

func (s *memEventStore) get(key string) *MCPEventSubscription {
	v, _ := s.Get(context.Background(), key)
	return v
}

// eventsTransport is a legacy-era mock server supporting events/*.
type eventsTransport struct {
	mu        sync.Mutex
	messages  chan *MCPMessage
	requests  []*MCPMessage
	events    bool
	listPages map[string]map[string]interface{}
	beforeSub func(params map[string]interface{})
	subErr    *MCPError
	cursor    interface{}
}

func newEventsTransport() *eventsTransport {
	return &eventsTransport{
		messages: make(chan *MCPMessage, 16),
		events:   true,
		cursor:   "cursor_1",
		listPages: map[string]map[string]interface{}{
			"": {"events": []interface{}{eventDefMap()}},
		},
	}
}

func eventDefMap() map[string]interface{} {
	return map[string]interface{}{
		"name": "comment.created", "delivery": []string{"webhook"},
		"inputSchema": map[string]interface{}{"type": "object"}, "payloadSchema": map[string]interface{}{"type": "object"},
	}
}

func (t *eventsTransport) Connect(context.Context) error { return nil }
func (t *eventsTransport) Close() error                  { return nil }
func (t *eventsTransport) IsConnected() bool             { return true }
func (t *eventsTransport) Receive(ctx context.Context) (*MCPMessage, error) {
	select {
	case m := <-t.messages:
		return m, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *eventsTransport) Send(_ context.Context, msg *MCPMessage) error {
	if msg.ID == nil {
		return nil
	}
	t.mu.Lock()
	t.requests = append(t.requests, msg)
	t.mu.Unlock()
	var params map[string]interface{}
	_ = json.Unmarshal(msg.Params, &params)
	resp := &MCPMessage{JSONRpc: "2.0", ID: msg.ID}
	var result interface{} = map[string]interface{}{}
	switch msg.Method {
	case "initialize":
		caps := map[string]interface{}{"tools": map[string]interface{}{}}
		if t.events {
			caps["events"] = map[string]interface{}{}
		}
		result = map[string]interface{}{"protocolVersion": LatestLegacyProtocolVersion, "capabilities": caps, "serverInfo": map[string]interface{}{"name": "s", "version": "1"}}
	case "events/list":
		cursor, _ := params["cursor"].(string)
		result = t.listPages[cursor]
	case "events/subscribe":
		if t.beforeSub != nil {
			t.beforeSub(params)
		}
		if t.subErr != nil {
			resp.Error = t.subErr
			t.messages <- resp
			return nil
		}
		r := map[string]interface{}{"id": "sub_1", "refreshBefore": time.Now().Add(time.Minute).UTC().Format(time.RFC3339), "truncated": false}
		if t.cursor != nil {
			r["cursor"] = t.cursor
		}
		result = r
	}
	resp.Result, _ = json.Marshal(result)
	t.messages <- resp
	return nil
}

func (t *eventsTransport) last(method string) map[string]interface{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := len(t.requests) - 1; i >= 0; i-- {
		if t.requests[i].Method == method {
			var p map[string]interface{}
			_ = json.Unmarshal(t.requests[i].Params, &p)
			return p
		}
	}
	return nil
}

func (t *eventsTransport) count(method string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, r := range t.requests {
		if r.Method == method {
			n++
		}
	}
	return n
}

func connectEventsClient(t *testing.T, tr *eventsTransport, cfg *MCPEventsConfig) *MCPClient {
	t.Helper()
	c := NewMCPClient(tr, MCPClientConfig{ExperimentalEvents: cfg})
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func strp(s string) *string { return &s }

var subOpts = SubscribeEventOptions{
	Name:      "comment.created",
	Arguments: map[string]interface{}{"document_id": "doc_1"},
	Delivery:  MCPEventWebhookDelivery{URL: "https://app.example/events?tenant=one"},
}

func signedEventRequest(sub *MCPEventSubscription, value interface{}, id string) *http.Request {
	body, _ := json.Marshal(value)
	return signedEventRequestBody(sub, body, id, fmt.Sprint(mcpEventNow().Unix()), sub.Delivery.Secret)
}

func signedEventRequestBody(sub *MCPEventSubscription, body []byte, id, ts, secret string) *http.Request {
	key, _ := base64.RawStdEncoding.DecodeString(strings.TrimRight(secret[6:], "="))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "." + string(body))) //nolint:errcheck
	r := httptest.NewRequest(http.MethodPost, sub.Delivery.URL, bytes.NewReader(body))
	r.Header.Set("content-type", "application/json")
	r.Header.Set("webhook-id", id)
	r.Header.Set("webhook-timestamp", ts)
	r.Header.Set("webhook-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	sid := sub.ID
	if sid == "" {
		sid = "sub_1"
	}
	r.Header.Set("X-MCP-Subscription-Id", sid)
	return r
}

func TestMCPEventsSubscribePersistsSecretBeforeRequestAndVerifies(t *testing.T) {
	store := newMemEventStore()
	tr := newEventsTransport()
	client := connectEventsClient(t, tr, &MCPEventsConfig{Store: store})
	webhook, err := ExperimentalCreateMCPEventWebhook(MCPEventWebhookOptions{
		Store: store,
		OnEvent: func(context.Context, MCPEventWebhookEventArgs) error {
			return errors.New("verification must not call OnEvent")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var verified int
	tr.beforeSub = func(params map[string]interface{}) {
		delivery := params["delivery"].(map[string]interface{})
		key := strings.Split(strings.Split(delivery["url"].(string), MCPEventCallbackKey+"=")[1], "&")[0]
		stored := store.get(key)
		if stored == nil || stored.Status != MCPEventStatusPending || stored.Delivery.Secret != delivery["secret"] {
			t.Errorf("pending record not persisted before subscribe: %#v", stored)
			return
		}
		if !strings.Contains(delivery["url"].(string), "tenant=one") {
			t.Errorf("query not preserved: %v", delivery["url"])
		}
		rec := httptest.NewRecorder()
		webhook.ServeHTTP(rec, signedEventRequest(stored, map[string]string{"type": "verification", "challenge": "c123"}, "msg_1"))
		if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"challenge":"c123"}` {
			t.Errorf("verification = %d %s", rec.Code, rec.Body.String())
		}
		verified++
	}
	result, err := client.ExperimentalEvents().Subscribe(context.Background(), subOpts)
	if err != nil {
		t.Fatal(err)
	}
	if verified != 1 || result.ID != "sub_1" || result.Cursor == nil || *result.Cursor != "cursor_1" {
		t.Fatalf("result = %#v verified=%d", result, verified)
	}
	stored, _ := store.GetByID(context.Background(), "sub_1")
	if stored == nil || stored.Status != MCPEventStatusActive {
		t.Fatalf("stored = %#v", stored)
	}
}

func TestMCPEventsRefreshUnsubscribeAndErrors(t *testing.T) {
	store := newMemEventStore()
	tr := newEventsTransport()
	tr.cursor = nil
	client := connectEventsClient(t, tr, &MCPEventsConfig{Store: store})
	ev := client.ExperimentalEvents()
	ctx := context.Background()
	res, err := ev.Subscribe(ctx, SubscribeEventOptions{Name: subOpts.Name, Delivery: subOpts.Delivery, TTLMs: NullableOf(int64(60000)), MaxAgeMs: func() *int64 { v := int64(5000); return &v }()})
	if err != nil || res.Cursor != nil {
		t.Fatalf("subscribe: %v %#v", err, res)
	}
	stored, _ := store.GetByID(ctx, "sub_1")
	_ = store.Update(ctx, stored.Key, MCPEventSubscriptionPatch{Cursor: NullableOf("from_delivery")})

	// Failed renewal keeps pending state.
	tr.subErr = &MCPError{Code: -32015, Message: "CallbackEndpointError"}
	_, err = ev.Refresh(ctx, MCPEventSubscriptionRef{ID: "sub_1"})
	var mErr *MCPClientError
	if !errors.As(err, &mErr) || mErr.Code != -32015 {
		t.Fatalf("err = %v", err)
	}
	if s := store.get(stored.Key); s.Status != MCPEventStatusPending {
		t.Fatalf("status = %s", s.Status)
	}
	tr.subErr = nil
	if _, err := ev.Refresh(ctx, MCPEventSubscriptionRef{ID: "sub_1"}); err != nil {
		t.Fatal(err)
	}
	p := tr.last("events/subscribe")
	if p["cursor"] != "from_delivery" || p["ttlMs"] != float64(60000) || p["maxAgeMs"] != float64(5000) {
		t.Fatalf("params = %#v", p)
	}
	if s := store.get(stored.Key); s.Status != MCPEventStatusActive || s.Cursor != nil {
		t.Fatalf("stored = %#v", s)
	}

	if err := ev.Unsubscribe(ctx, MCPEventSubscriptionRef{ID: "sub_1"}); err != nil {
		t.Fatal(err)
	}
	up := tr.last("events/unsubscribe")
	if _, hasID := up["id"]; hasID || up["delivery"].(map[string]interface{})["url"] != stored.Delivery.URL {
		t.Fatalf("unsubscribe params = %#v", up)
	}
	if _, hasSecret := up["delivery"].(map[string]interface{})["secret"]; hasSecret {
		t.Fatal("unsubscribe must not send the secret")
	}
	if store.get(stored.Key) != nil {
		t.Fatal("record should be deleted")
	}
	before := tr.count("events/subscribe")
	if _, err := ev.Refresh(ctx, MCPEventSubscriptionRef{Key: "missing"}); err == nil || !strings.Contains(err.Error(), "Unknown MCP event subscription: missing") {
		t.Fatalf("err = %v", err)
	}
	if tr.count("events/subscribe") != before {
		t.Fatal("request sent for unknown key")
	}
}

func TestMCPEventsExplicitNullTTLIsSentAndPersisted(t *testing.T) {
	store := newMemEventStore()
	tr := newEventsTransport()
	client := connectEventsClient(t, tr, &MCPEventsConfig{Store: store})
	tr.beforeSub = func(map[string]interface{}) {}
	opts := subOpts
	opts.TTLMs = NullableNull[int64]()
	if _, err := client.ExperimentalEvents().Subscribe(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	p := tr.last("events/subscribe")
	if v, ok := p["ttlMs"]; !ok || v != nil {
		t.Fatalf("ttlMs = %v present=%v", v, ok)
	}
	if _, ok := p["delivery"].(map[string]interface{})["allowInsecureLocalhost"]; ok {
		t.Fatal("dev option forwarded")
	}
}

func TestMCPEventsValidation(t *testing.T) {
	store := newMemEventStore()
	tr := newEventsTransport()
	validated := false
	client := connectEventsClient(t, tr, &MCPEventsConfig{Store: store, ValidateArguments: func(_ context.Context, d MCPEventDefinition, a map[string]interface{}) error {
		validated = true
		if len(a) == 0 {
			return errors.New("arguments required")
		}
		return nil
	}})
	ev := client.ExperimentalEvents()
	ctx := context.Background()

	opts := subOpts
	opts.Arguments = nil
	if _, err := ev.Subscribe(ctx, opts); err == nil || !validated || len(store.records) != 0 || tr.count("events/subscribe") != 0 {
		t.Fatalf("validation did not block: %v", err)
	}
	for _, url := range []string{"http://app.example/events", "https://user:password@app.example/events", "https://app.example/events#fragment", "http://127.0.0.1:3003/events"} {
		o := subOpts
		o.Delivery.URL = url
		if _, err := ev.Subscribe(ctx, o); err == nil || !strings.Contains(err.Error(), "HTTPS") {
			t.Fatalf("%s: %v", url, err)
		}
	}
	o := subOpts
	o.Delivery = MCPEventWebhookDelivery{URL: "http://127.0.0.1:3003/events", AllowInsecureLocalhost: true}
	if _, err := ev.Subscribe(ctx, o); err != nil {
		t.Fatalf("loopback: %v", err)
	}
	o.Delivery = MCPEventWebhookDelivery{URL: "http://app.example/events", AllowInsecureLocalhost: true}
	if _, err := ev.Subscribe(ctx, o); err == nil {
		t.Fatal("non-loopback http accepted")
	}
	o = subOpts
	o.Delivery.Secret = "whsec_c2hvcnQ="
	if _, err := ev.Subscribe(ctx, o); err == nil || !strings.Contains(err.Error(), "24–64") {
		t.Fatalf("secret: %v", err)
	}
	neg := int64(-1)
	o = subOpts
	o.TTLMs = NullableOf(neg)
	if _, err := ev.Subscribe(ctx, o); err == nil || !strings.Contains(err.Error(), "lifetime") {
		t.Fatalf("ttl: %v", err)
	}
	o = subOpts
	o.MaxAgeMs = &neg
	if _, err := ev.Subscribe(ctx, o); err == nil || !strings.Contains(err.Error(), "lifetime") {
		t.Fatalf("maxAge: %v", err)
	}
}

func TestMCPEventsCatalogPaginationAndDelivery(t *testing.T) {
	store := newMemEventStore()
	tr := newEventsTransport()
	client := connectEventsClient(t, tr, &MCPEventsConfig{Store: store})
	ev := client.ExperimentalEvents()
	ctx := context.Background()
	tr.listPages = map[string]map[string]interface{}{
		"":       {"events": []interface{}{}, "nextCursor": "page_2"},
		"page_2": {"events": []interface{}{eventDefMap()}},
	}
	if _, err := ev.Subscribe(ctx, subOpts); err != nil {
		t.Fatal(err)
	}
	if tr.count("events/list") != 2 {
		t.Fatalf("list calls = %d", tr.count("events/list"))
	}
	tr.listPages = map[string]map[string]interface{}{"": {"events": []interface{}{}, "nextCursor": "same"}, "same": {"events": []interface{}{}, "nextCursor": "same"}}
	if _, err := ev.Subscribe(ctx, subOpts); err == nil || !strings.Contains(err.Error(), "Repeated") {
		t.Fatalf("err = %v", err)
	}
	poll := eventDefMap()
	poll["delivery"] = []string{"poll"}
	tr.listPages = map[string]map[string]interface{}{"": {"events": []interface{}{poll}}}
	if _, err := ev.Subscribe(ctx, subOpts); err == nil || !strings.Contains(err.Error(), "does not support webhook") {
		t.Fatalf("err = %v", err)
	}
}

func TestMCPEventsRequireServerCapabilityAndStore(t *testing.T) {
	tr := newEventsTransport()
	tr.events = false
	client := connectEventsClient(t, tr, &MCPEventsConfig{Store: newMemEventStore()})
	if _, err := client.ExperimentalEvents().List(context.Background(), ListEventsOptions{}); err == nil || !strings.Contains(err.Error(), "Server does not support events") {
		t.Fatalf("err = %v", err)
	}
	if tr.count("events/list") != 0 {
		t.Fatal("request sent")
	}

	tr2 := newEventsTransport()
	c2 := connectEventsClient(t, tr2, nil)
	if res, err := c2.ExperimentalEvents().List(context.Background(), ListEventsOptions{}); err != nil || len(res.Events) != 1 {
		t.Fatalf("list: %v", err)
	}
	if _, err := c2.ExperimentalEvents().Subscribe(context.Background(), subOpts); err == nil || !strings.Contains(err.Error(), "experimental_events.store") {
		t.Fatalf("err = %v", err)
	}
}

// ---- webhook ----

type webhookHarness struct {
	store   *memEventStore
	sub     *MCPEventSubscription
	handler http.Handler
	mu      sync.Mutex
	events  int
	gaps    []MCPEventWebhookGapArgs
	terms   []MCPEventWebhookTerminatedArgs
	errs    int
	failCb  error
}

func newWebhookHarness(t *testing.T) *webhookHarness {
	t.Helper()
	fixed := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	old := mcpEventNow
	mcpEventNow = func() time.Time { return fixed }
	t.Cleanup(func() { mcpEventNow = old })

	h := &webhookHarness{store: newMemEventStore()}
	secret := "whsec_" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	h.sub = &MCPEventSubscription{
		Key: "local_1", ID: "sub_1", Name: "comment.created", Arguments: map[string]interface{}{"document_id": "doc_1"},
		Definition: testEventDefinition,
		Delivery:   MCPEventDelivery{Mode: "webhook", URL: "https://app.example/events?mcp_event_subscription=local_1", Secret: secret},
		Cursor:     strp("cursor_1"), RefreshBefore: strp(fixed.Add(time.Minute).Format(time.RFC3339)), Status: MCPEventStatusActive,
	}
	_ = h.store.Set(context.Background(), *h.sub)
	var err error
	h.handler, err = ExperimentalCreateMCPEventWebhook(MCPEventWebhookOptions{
		Store: h.store,
		ValidatePayload: func(_ context.Context, _ MCPEventDefinition, data map[string]interface{}) error {
			if _, ok := data["text"].(string); !ok || len(data) != 1 {
				return errors.New("bad payload")
			}
			return nil
		},
		OnEvent: func(context.Context, MCPEventWebhookEventArgs) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.failCb != nil {
				return h.failCb
			}
			h.events++
			return nil
		},
		OnGap: func(_ context.Context, a MCPEventWebhookGapArgs) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.failCb != nil {
				return h.failCb
			}
			h.gaps = append(h.gaps, a)
			return nil
		},
		OnTerminated: func(_ context.Context, a MCPEventWebhookTerminatedArgs) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.failCb != nil {
				return h.failCb
			}
			h.terms = append(h.terms, a)
			return nil
		},
		OnError: func(error) { h.mu.Lock(); h.errs++; h.mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *webhookHarness) do(r *http.Request) int {
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, r)
	return rec.Code
}

var testEvent = map[string]interface{}{
	"eventId": "evt_1", "name": "comment.created", "timestamp": "2026-10-05T12:00:00Z",
	"data": map[string]interface{}{"text": "Please review ✨"}, "cursor": "cursor_2",
}

func TestMCPEventWebhookDeliversSignedEvent(t *testing.T) {
	h := newWebhookHarness(t)
	body, _ := json.MarshalIndent(testEvent, "", "  ")
	r := signedEventRequestBody(h.sub, body, "evt_1", fmt.Sprint(mcpEventNow().Unix()), h.sub.Delivery.Secret)
	if code := h.do(r); code != 204 || h.events != 1 {
		t.Fatalf("code=%d events=%d", code, h.events)
	}
	if s := h.store.get("local_1"); s.Cursor == nil || *s.Cursor != "cursor_2" {
		t.Fatalf("cursor = %v", s.Cursor)
	}
}

func TestMCPEventWebhookRejections(t *testing.T) {
	h := newWebhookHarness(t)
	now := mcpEventNow().Unix()
	ts := func(d int64) string { return fmt.Sprint(now + d) }
	body, _ := json.Marshal(testEvent)

	// Tampered body.
	r := signedEventRequest(h.sub, testEvent, "evt_1")
	tampered := httptest.NewRequest(http.MethodPost, r.URL.String(), strings.NewReader(`{"x":1}`))
	tampered.Header = r.Header
	if code := h.do(tampered); code != 401 {
		t.Fatalf("tampered = %d", code)
	}
	// Timestamp tolerance.
	for d, want := range map[int64]int{-300: 204, 300: 204, -301: 401, 301: 401} {
		if code := h.do(signedEventRequestBody(h.sub, body, "evt_1", ts(d), h.sub.Delivery.Secret)); code != want {
			t.Fatalf("skew %d = %d, want %d", d, code, want)
		}
	}
	// Missing headers.
	for _, hdr := range []string{"webhook-id", "webhook-timestamp", "webhook-signature", "X-MCP-Subscription-Id"} {
		r := signedEventRequest(h.sub, testEvent, "evt_1")
		r.Header.Del(hdr)
		if code := h.do(r); code != 401 {
			t.Fatalf("missing %s = %d", hdr, code)
		}
	}
	// Rotation: valid signature after malformed ones.
	r = signedEventRequest(h.sub, testEvent, "evt_1")
	r.Header.Set("webhook-signature", "v1,malformed v2,unknown "+r.Header.Get("webhook-signature"))
	if code := h.do(r); code != 204 {
		t.Fatalf("rotation = %d", code)
	}
	// Another secret.
	other := "whsec_" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	if code := h.do(signedEventRequestBody(h.sub, body, "evt_1", ts(0), other)); code != 401 {
		t.Fatalf("other secret = %d", code)
	}
	// Substituted subscription header / unknown key.
	r = signedEventRequest(h.sub, testEvent, "evt_1")
	r.Header.Set("X-MCP-Subscription-Id", "sub_other")
	if code := h.do(r); code != 401 {
		t.Fatalf("subst = %d", code)
	}
	u := httptest.NewRequest(http.MethodPost, "https://app.example/events?mcp_event_subscription=missing", strings.NewReader("{}"))
	u.Header = signedEventRequest(h.sub, testEvent, "evt_1").Header
	if code := h.do(u); code != 401 {
		t.Fatalf("unknown key = %d", code)
	}
	// Invalid envelopes/payloads.
	for _, mod := range []func(m map[string]interface{}){
		func(m map[string]interface{}) { m["name"] = "other.event" },
		func(m map[string]interface{}) { m["eventId"] = "evt_other" },
		func(m map[string]interface{}) { m["timestamp"] = "invalid" },
		func(m map[string]interface{}) { m["data"] = map[string]interface{}{"text": 42} },
		func(m map[string]interface{}) { m["data"] = map[string]interface{}{"text": "a", "unexpected": true} },
	} {
		m := map[string]interface{}{}
		for k, v := range testEvent {
			m[k] = v
		}
		mod(m)
		if code := h.do(signedEventRequest(h.sub, m, "evt_1")); code != 400 {
			t.Fatalf("invalid envelope = %d (%v)", code, m)
		}
	}
	if s := h.store.get("local_1"); *s.Cursor != "cursor_2" { // advanced only by the accepted calls above
		t.Fatalf("cursor = %v", *s.Cursor)
	}
	// Signed invalid JSON.
	if code := h.do(signedEventRequestBody(h.sub, []byte("{ invalid"), "evt_1", ts(0), h.sub.Delivery.Secret)); code != 400 {
		t.Fatalf("invalid json = %d", code)
	}
	// Method / content type.
	if code := h.do(httptest.NewRequest(http.MethodGet, h.sub.Delivery.URL, nil)); code != 405 {
		t.Fatalf("get = %d", code)
	}
	r = signedEventRequest(h.sub, testEvent, "evt_1")
	r.Header.Set("content-type", "text/plain")
	if code := h.do(r); code != 415 {
		t.Fatalf("ct = %d", code)
	}
}

func TestMCPEventWebhookBodyLimits(t *testing.T) {
	h := newWebhookHarness(t)
	big := bytes.Repeat([]byte(" "), 256*1024+1)
	r := signedEventRequestBody(h.sub, big, "evt_1", fmt.Sprint(mcpEventNow().Unix()), h.sub.Delivery.Secret)
	r.ContentLength = -1
	if code := h.do(r); code != 413 {
		t.Fatalf("stream = %d", code)
	}
	r = signedEventRequest(h.sub, testEvent, "evt_1")
	r.ContentLength = 256*1024 + 1
	if code := h.do(r); code != 413 {
		t.Fatalf("content-length = %d", code)
	}
}

func TestMCPEventWebhookStateGating(t *testing.T) {
	ctx := context.Background()
	h := newWebhookHarness(t)
	pending := MCPEventStatusPending
	_ = h.store.Update(ctx, "local_1", MCPEventSubscriptionPatch{Status: &pending})
	if code := h.do(signedEventRequest(h.sub, testEvent, "evt_1")); code != 503 || h.events != 0 {
		t.Fatalf("pending event = %d", code)
	}
	gap := map[string]interface{}{"type": "gap", "cursor": "cursor_fresh"}
	if code := h.do(signedEventRequest(h.sub, gap, "m1")); code != 503 || len(h.gaps) != 0 {
		t.Fatalf("pending gap = %d", code)
	}
	// Signed verification is echoed while pending.
	if code := h.do(signedEventRequest(h.sub, map[string]string{"type": "verification", "challenge": "n"}, "m2")); code != 200 {
		t.Fatalf("verification = %d", code)
	}
	// Termination ends a pending renewal.
	term := map[string]interface{}{"type": "terminated", "error": map[string]interface{}{"code": -32012, "message": "Forbidden", "data": map[string]interface{}{"reason": "x"}}}
	h.failCb = errors.New("queue unavailable")
	if code := h.do(signedEventRequest(h.sub, term, "m3")); code != 503 || h.store.get("local_1") == nil || h.errs != 1 {
		t.Fatalf("failing termination = %d", code)
	}
	h.failCb = nil
	if code := h.do(signedEventRequest(h.sub, term, "m3")); code != 204 || len(h.terms) != 1 || h.store.get("local_1") != nil {
		t.Fatalf("termination = %d", code)
	}
	if h.terms[0].MessageID != "m3" || h.terms[0].Subscription.ID != "sub_1" || h.terms[0].Termination.Error.Code != -32012 {
		t.Fatalf("terms = %#v", h.terms[0])
	}
}

func TestMCPEventWebhookGapAndExpiry(t *testing.T) {
	ctx := context.Background()
	h := newWebhookHarness(t)
	gap := map[string]interface{}{"type": "gap", "cursor": "cursor_fresh"}
	// Gaps are handled even after local expiration.
	_ = h.store.Update(ctx, "local_1", MCPEventSubscriptionPatch{RefreshBefore: NullableOf(mcpEventNow().Add(-time.Second).Format(time.RFC3339))})
	if code := h.do(signedEventRequest(h.sub, gap, "m1")); code != 204 || len(h.gaps) != 1 || !h.gaps[0].Gap.Truncated {
		t.Fatalf("gap = %d", code)
	}
	if s := h.store.get("local_1"); *s.Cursor != "cursor_fresh" || !s.Truncated {
		t.Fatalf("stored = %#v", s)
	}
	if code := h.do(signedEventRequest(h.sub, testEvent, "evt_1")); code != 410 || h.events != 0 {
		t.Fatalf("expired = %d", code)
	}
	_ = h.store.Update(ctx, "local_1", MCPEventSubscriptionPatch{RefreshBefore: NullableNull[string]()})
	if code := h.do(signedEventRequest(h.sub, testEvent, "evt_1")); code != 204 {
		t.Fatalf("no expiry = %d", code)
	}
	// Storage failure on control -> 503.
	h.store.failUpd = errors.New("db down")
	if code := h.do(signedEventRequest(h.sub, gap, "m2")); code != 503 {
		t.Fatalf("storage failure = %d", code)
	}
	h.store.failUpd = nil
	// Invalid control envelopes -> 400.
	for _, c := range []map[string]interface{}{
		{"type": "unknown", "cursor": "c"}, {"type": "gap"}, {"type": "gap", "cursor": 42}, {"type": "terminated"},
		{"type": "terminated", "error": map[string]interface{}{"code": "invalid", "message": "x"}},
	} {
		if code := h.do(signedEventRequest(h.sub, c, "m")); code != 400 {
			t.Fatalf("invalid control %v = %d", c, code)
		}
	}
	// OnEvent failure -> 503 and no cursor advance.
	h2 := newWebhookHarness(t)
	h2.failCb = errors.New("queue")
	if code := h2.do(signedEventRequest(h2.sub, testEvent, "evt_1")); code != 503 || *h2.store.get("local_1").Cursor != "cursor_1" {
		t.Fatalf("event failure = %d", code)
	}
}

// ---- managed adapter ----

type fakeOps struct {
	mu    sync.Mutex
	calls []string
	sub   *ManagedSubscription
	err   error
	last  ManagedSubscribeInput
}

func (f *fakeOps) rec(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }
func (f *fakeOps) Subscribe(_ context.Context, in ManagedSubscribeInput) (*ManagedSubscription, error) {
	f.rec("subscribe")
	f.last = in
	return f.sub, f.err
}
func (f *fakeOps) GetSubscription(context.Context, string) (*ManagedSubscription, error) {
	f.rec("get")
	return f.sub, f.err
}
func (f *fakeOps) ListSubscriptions(context.Context, ListManagedSubscriptionsInput) (*ListManagedSubscriptionsResult, error) {
	f.rec("list")
	return &ListManagedSubscriptionsResult{Subscriptions: []ManagedSubscription{*f.sub}}, f.err
}
func (f *fakeOps) Unsubscribe(context.Context, string) (*ManagedSubscription, error) {
	f.rec("unsubscribe")
	return f.sub, f.err
}

type fakeAdapter struct {
	ops   MCPEventOperations
	err   error
	ctxs  []MCPEventAdapterContext
	calls int
}

func (a *fakeAdapter) CreateAdapter(c MCPEventAdapterContext) (MCPEventOperations, error) {
	a.calls++
	a.ctxs = append(a.ctxs, c)
	return a.ops, a.err
}

func TestManagedMCPEventsDelegateToAdapter(t *testing.T) {
	ops := &fakeOps{sub: &ManagedSubscription{ID: "managed_1", Name: "comment.created", Status: ManagedSubscriptionPending}}
	adapter := &fakeAdapter{ops: ops}
	tr := newEventsTransport()
	client := connectEventsClient(t, tr, &MCPEventsConfig{Adapter: adapter})
	managed := client.ExperimentalManagedEvents()
	if managed == nil || adapter.calls != 1 || adapter.ctxs[0].Transport.Type != MCPEventTransportCustom || adapter.ctxs[0].Transport.URL != "" {
		t.Fatalf("adapter ctx = %#v", adapter.ctxs)
	}
	ctx := context.Background()
	before := tr.count("events/subscribe")
	got, err := managed.Subscribe(ctx, ManagedSubscribeInput{Name: "comment.created", IdempotencyKey: "intent_1", TTLMs: NullableNull[int64]()})
	if err != nil || got != ops.sub || ops.last.IdempotencyKey != "intent_1" || !ops.last.TTLMs.Set || ops.last.TTLMs.Value != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := managed.GetSubscription(ctx, "managed_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := managed.ListSubscriptions(ctx, ListManagedSubscriptionsInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := managed.Unsubscribe(ctx, "managed_1"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ops.calls, ",") != "subscribe,get,list,unsubscribe" || tr.count("events/subscribe") != before {
		t.Fatalf("calls = %v", ops.calls)
	}
	// Catalog still goes over MCP, not the adapter.
	if res, err := managed.List(ctx, ListEventsOptions{}); err != nil || len(res.Events) != 1 || len(ops.calls) != 4 {
		t.Fatalf("list: %v", err)
	}
	// Adapter errors are preserved as-is.
	ops.err = errors.New("Consent required")
	if _, err := managed.Subscribe(ctx, ManagedSubscribeInput{}); err != ops.err {
		t.Fatalf("err = %v", err)
	}
}

func TestManagedMCPEventsAdapterBinding(t *testing.T) {
	ops := &fakeOps{sub: &ManagedSubscription{ID: "m"}}
	for _, tc := range []struct {
		name string
		tr   Transport
		want MCPEventAdapterTransport
	}{
		{"http", NewHTTPTransport(HTTPTransportConfig{URL: "https://example.com/tenant/mcp?region=west"}), MCPEventAdapterTransport{Type: "http", URL: "https://example.com/tenant/mcp?region=west"}},
		{"sse", NewSSETransport(SSETransportConfig{URL: "https://example.com/sse"}), MCPEventAdapterTransport{Type: "sse", URL: "https://example.com/sse"}},
	} {
		adapter := &fakeAdapter{ops: ops}
		c := NewMCPClient(tc.tr, MCPClientConfig{ExperimentalEvents: &MCPEventsConfig{Adapter: adapter}})
		if adapter.calls != 1 || adapter.ctxs[0].Transport != tc.want || c.ExperimentalManagedEvents() == nil {
			t.Fatalf("%s: %#v", tc.name, adapter.ctxs)
		}
	}
}

func TestManagedMCPEventsConfigErrorsSurfaceBeforeTransportStarts(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg  *MCPEventsConfig
		want string
		is   error
	}{
		"mixed store":    {&MCPEventsConfig{Adapter: &fakeAdapter{ops: &fakeOps{}}, Store: newMemEventStore()}, "either an adapter or a store", nil},
		"mixed validate": {&MCPEventsConfig{Adapter: &fakeAdapter{ops: &fakeOps{}}, ValidateArguments: func(context.Context, MCPEventDefinition, map[string]interface{}) error { return nil }}, "either an adapter or a store", nil},
		"nil operations": {&MCPEventsConfig{Adapter: &fakeAdapter{}}, "must return an object implementing", nil},
	} {
		t.Run(name, func(t *testing.T) {
			tr := &countingConnectTransport{eventsTransport: newEventsTransport()}
			c := NewMCPClient(tr, MCPClientConfig{ExperimentalEvents: tc.cfg})
			var mErr *MCPClientError
			err := c.Connect(context.Background())
			if !errors.As(err, &mErr) || !strings.Contains(err.Error(), tc.want) || tr.connects != 0 {
				t.Fatalf("err = %v connects=%d", err, tr.connects)
			}
		})
	}
	sentinel := errors.New("Missing endpoint configuration")
	tr := &countingConnectTransport{eventsTransport: newEventsTransport()}
	adapter := &fakeAdapter{err: sentinel}
	c := NewMCPClient(tr, MCPClientConfig{ExperimentalEvents: &MCPEventsConfig{Adapter: adapter}})
	if err := c.Connect(context.Background()); err != sentinel || tr.connects != 0 {
		t.Fatalf("err = %v", err)
	}
	// Mixed config must not invoke the adapter.
	a2 := &fakeAdapter{ops: &fakeOps{}}
	NewMCPClient(tr, MCPClientConfig{ExperimentalEvents: &MCPEventsConfig{Adapter: a2, Store: newMemEventStore()}})
	if a2.calls != 0 {
		t.Fatal("adapter invoked for mixed config")
	}
}

type countingConnectTransport struct {
	*eventsTransport
	connects int
}

func (c *countingConnectTransport) Connect(context.Context) error { c.connects++; return nil }

func TestMCPEventSignatureRoundTrip(t *testing.T) {
	secret, err := createMCPEventSecret()
	if err != nil {
		t.Fatal(err)
	}
	if k, err := decodeMCPEventSecret(secret); err != nil || len(k) != 32 {
		t.Fatalf("decode: %v", err)
	}
	if _, err := decodeMCPEventSecret("nope"); err == nil {
		t.Fatal("expected invalid secret error")
	}
}

// Review: a request that fails authentication or size checks must never reach
// application callbacks or mutate the stored cursor.
func TestMCPEventWebhookUnauthenticatedRequestsHaveNoSideEffects(t *testing.T) {
	h := newWebhookHarness(t)
	now := mcpEventNow().Unix()
	body, _ := json.Marshal(testEvent)
	other := "whsec_" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))

	cases := map[string]*http.Request{
		"forged":      signedEventRequestBody(h.sub, body, "evt_1", fmt.Sprint(now), other),
		"stale":       signedEventRequestBody(h.sub, body, "evt_1", fmt.Sprint(now-3600), h.sub.Delivery.Secret),
		"future":      signedEventRequestBody(h.sub, body, "evt_1", fmt.Sprint(now+3600), h.sub.Delivery.Secret),
		"exponent":    signedEventRequestBody(h.sub, body, "evt_1", "1e9", h.sub.Delivery.Secret),
		"signed-plus": signedEventRequestBody(h.sub, body, "evt_1", "+"+fmt.Sprint(now), h.sub.Delivery.Secret),
		"huge":        signedEventRequestBody(h.sub, body, "evt_1", strings.Repeat("9", 400), h.sub.Delivery.Secret),
	}
	big := signedEventRequestBody(h.sub, bytes.Repeat([]byte("a"), 256*1024+1), "evt_1", fmt.Sprint(now), h.sub.Delivery.Secret)
	big.ContentLength = -1
	cases["oversized"] = big
	for name, r := range cases {
		want := 401
		if name == "oversized" {
			want = 413
		}
		if code := h.do(r); code != want {
			t.Fatalf("%s = %d, want %d", name, code, want)
		}
	}
	// Forged control messages must not delete the subscription or move cursors.
	for _, ctl := range []map[string]interface{}{
		{"type": "gap", "cursor": "evil"},
		{"type": "terminated", "error": map[string]interface{}{"code": -32012, "message": "x"}},
	} {
		cb, _ := json.Marshal(ctl)
		if code := h.do(signedEventRequestBody(h.sub, cb, "m", fmt.Sprint(now), other)); code != 401 {
			t.Fatalf("forged control = %d", code)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.store.get("local_1")
	if h.events != 0 || len(h.gaps) != 0 || len(h.terms) != 0 || h.errs != 0 || s == nil || *s.Cursor != "cursor_1" {
		t.Fatalf("side effects: events=%d gaps=%d terms=%d errs=%d sub=%v", h.events, len(h.gaps), len(h.terms), h.errs, s)
	}
}

// TS paginates while nextCursor != null, so an empty-string nextCursor is
// followed (as cursor: "") and a repeat of it is a repeated cursor.
func TestMCPEventsCatalogFollowsEmptyStringNextCursor(t *testing.T) {
	tr := newEventsTransport()
	client := connectEventsClient(t, tr, &MCPEventsConfig{Store: newMemEventStore()})
	tr.listPages = map[string]map[string]interface{}{"": {"events": []interface{}{}, "nextCursor": ""}}
	_, err := client.ExperimentalEvents().Subscribe(context.Background(), subOpts)
	if err == nil || !strings.Contains(err.Error(), "Repeated MCP events pagination cursor") {
		t.Fatalf("err = %v, want repeated cursor", err)
	}
	if tr.count("events/list") != 2 {
		t.Fatalf("list calls = %d, want 2", tr.count("events/list"))
	}
	p := tr.last("events/list")
	if c, ok := p["cursor"]; !ok || c != "" {
		t.Fatalf("second list params = %v, want cursor \"\"", p)
	}
}
