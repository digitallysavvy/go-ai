package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// r85toolsMRTRTransport answers server/discover with a modern server and
// every other request with results[min(i, len-1)], recording the requests
// (TS mcp-input-required.test.ts setup).
type r85toolsMRTRTransport struct {
	mu       sync.Mutex
	messages chan *MCPMessage
	closed   bool
	results  []string
	requests []*MCPMessage
	// discoverErr, when set, answers server/discover with this JSON-RPC
	// error object.
	discoverErr string
	methods     []string
}

func (t *r85toolsMRTRTransport) Connect(context.Context) error          { return nil }
func (t *r85toolsMRTRTransport) IsConnected() bool                      { return true }
func (t *r85toolsMRTRTransport) SetProtocolVersion(string)              {}
func (t *r85toolsMRTRTransport) SupportsProtocolVersionDiscovery() bool { return true }
func (t *r85toolsMRTRTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.messages)
	}
	return nil
}

func (t *r85toolsMRTRTransport) Send(_ context.Context, msg *MCPMessage) error {
	if msg.Method == "" || msg.ID == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errors.New("closed")
	}
	t.methods = append(t.methods, msg.Method)
	if msg.Method == "server/discover" && t.discoverErr != "" {
		reply, err := parseMCPMessage([]byte(`{"jsonrpc":"2.0","id":` + string(r85toolsJSON(msg.ID)) + `,"error":` + t.discoverErr + `}`))
		if err != nil {
			return err
		}
		t.messages <- reply
		return nil
	}
	result := `{"resultType":"complete","supportedVersions":["2026-07-28"],"capabilities":{"tools":{},"resources":{},"prompts":{}}}`
	if msg.Method != "server/discover" {
		t.requests = append(t.requests, msg)
		i := len(t.requests) - 1
		if i >= len(t.results) {
			i = len(t.results) - 1
		}
		result = t.results[i]
	}
	t.messages <- &MCPMessage{JSONRpc: "2.0", ID: msg.ID, Result: json.RawMessage(result)}
	return nil
}

func (t *r85toolsMRTRTransport) Receive(ctx context.Context) (*MCPMessage, error) {
	select {
	case msg, ok := <-t.messages:
		if !ok {
			return nil, context.Canceled
		}
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *r85toolsMRTRTransport) recorded() []*MCPMessage {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*MCPMessage(nil), t.requests...)
}

const (
	r85toolsElicitation = `{"method":"elicitation/create","params":{"message":"Proceed?","requestedSchema":{"type":"object"}}}`
	r85toolsComplete    = `{"resultType":"complete","content":[],"contents":[],"messages":[]}`
)

func r85toolsSetup(t *testing.T, results ...string) (*MCPClient, *r85toolsMRTRTransport) {
	t.Helper()
	tr := &r85toolsMRTRTransport{messages: make(chan *MCPMessage, 64), results: results}
	c := NewMCPClient(tr, MCPClientConfig{Capabilities: ClientCapabilities{Elicitation: map[string]interface{}{}}})
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, tr
}

func r85toolsParams(t *testing.T, m *MCPMessage) map[string]interface{} {
	t.Helper()
	var p map[string]interface{}
	if err := json.Unmarshal(m.Params, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// Ports "continues a %s request with fresh IDs and only the latest
// answers/state".
func TestR85toolsInputRequiredContinues(t *testing.T) {
	for _, kind := range []string{"tool", "resource", "prompt"} {
		t.Run(kind, func(t *testing.T) {
			c, tr := r85toolsSetup(t,
				`{"resultType":"input_required","inputRequests":{"confirm":`+r85toolsElicitation+`},"requestState":" opaque state\n"}`,
				`{"resultType":"input_required","inputRequests":{"again":`+r85toolsElicitation+`}}`,
				r85toolsComplete,
			)
			var mu sync.Mutex
			var seen []ElicitationRequest
			answers := []ElicitResult{{Action: "accept", Content: map[string]interface{}{"ok": true}}, {Action: "decline"}}
			c.OnElicitationRequest(func(_ context.Context, req ElicitationRequest) (ElicitResult, error) {
				mu.Lock()
				defer mu.Unlock()
				seen = append(seen, req)
				return answers[len(seen)-1], nil
			})
			ctx := context.Background()
			var err error
			switch kind {
			case "tool":
				_, err = c.CallTool(ctx, "deploy", map[string]interface{}{"target": "demo"})
			case "resource":
				_, err = c.ReadResource(ctx, "file:///demo")
			default:
				_, err = c.GetPrompt(ctx, "deploy", map[string]interface{}{"target": "demo"})
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(seen) != 2 || seen[0].Message != "Proceed?" || !reflect.DeepEqual(seen[0].RequestedSchema, map[string]interface{}{"type": "object"}) {
				t.Fatalf("handler calls = %+v", seen)
			}
			reqs := tr.recorded()
			if len(reqs) != 3 {
				t.Fatalf("requests = %d", len(reqs))
			}
			ids := map[string]bool{}
			for _, r := range reqs {
				ids[string(r85toolsJSON(r.ID))] = true
				if r.Method != reqs[0].Method {
					t.Fatalf("method %s != %s", r.Method, reqs[0].Method)
				}
			}
			if len(ids) != 3 {
				t.Fatalf("request ids not fresh: %v", ids)
			}
			base := r85toolsParams(t, reqs[0])
			want1 := map[string]interface{}{}
			for k, v := range base {
				want1[k] = v
			}
			want1["inputResponses"] = map[string]interface{}{"confirm": map[string]interface{}{"action": "accept", "content": map[string]interface{}{"ok": true}}}
			want1["requestState"] = " opaque state\n"
			if got := r85toolsParams(t, reqs[1]); !reflect.DeepEqual(got, want1) {
				t.Fatalf("round 2 params = %v\nwant %v", got, want1)
			}
			want2 := map[string]interface{}{}
			for k, v := range base {
				want2[k] = v
			}
			want2["inputResponses"] = map[string]interface{}{"again": map[string]interface{}{"action": "decline"}}
			if got := r85toolsParams(t, reqs[2]); !reflect.DeepEqual(got, want2) {
				t.Fatalf("round 3 params = %v\nwant %v", got, want2)
			}
		})
	}
}

func r85toolsJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

// Ports "rejects unsupported or invalid input requirements".
func TestR85toolsInputRequiredRejects(t *testing.T) {
	for _, tc := range []struct {
		result, want string
	}{
		{`{"inputRequests":{"confirm":` + r85toolsElicitation + `}}`, "onElicitationRequest"},
		{`{"inputRequests":{"sample":{"method":"sampling/createMessage"}}}`, "Unsupported input request"},
		{`{"inputRequests":{"confirm":{"method":"elicitation/create","params":{"mode":"url"}}}}`, "Unsupported input request"},
		{`{"requestState":42}`, "Failed to process"},
		{`{}`, "Failed to process"},
	} {
		result := `{"resultType":"input_required"}`
		if tc.result != "{}" {
			result = `{"resultType":"input_required",` + strings.TrimPrefix(tc.result, "{")
		}
		c, tr := r85toolsSetup(t, result)
		_, err := c.CallTool(context.Background(), "deploy", nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want %q", tc.result, err, tc.want)
		}
		if n := len(tr.recorded()); n != 1 {
			t.Fatalf("%s: requests = %d, want 1", tc.result, n)
		}
	}
}

// Ports "paces state-only retries and stops after ten rounds".
func TestR85toolsInputRequiredPacesAndStops(t *testing.T) {
	c, tr := r85toolsSetup(t, `{"resultType":"input_required","requestState":""}`)
	start := time.Now()
	errCh := make(chan error, 1)
	go func() {
		_, err := c.CallTool(context.Background(), "deploy", nil)
		errCh <- err
	}()
	time.Sleep(150 * time.Millisecond)
	if n := len(tr.recorded()); n != 1 {
		t.Fatalf("requests before the 250 ms delay = %d, want 1", n)
	}
	err := <-errCh
	if err == nil || err.Error() != "Request still requires input after 10 rounds" {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 2500*time.Millisecond {
		t.Fatalf("10 paced retries took %v, want >= 2.5s", elapsed)
	}
	reqs := tr.recorded()
	if len(reqs) != 11 {
		t.Fatalf("requests = %d, want 11", len(reqs))
	}
	want := r85toolsParams(t, reqs[0])
	want["requestState"] = ""
	if got := r85toolsParams(t, reqs[1]); !reflect.DeepEqual(got, want) {
		t.Fatalf("retry params = %v, want %v", got, want)
	}
}

// Ports "stops while awaiting input on abort / close / timeout".
func TestR85toolsInputRequiredStopsWhileAwaitingInput(t *testing.T) {
	for _, stop := range []string{"abort", "close", "timeout"} {
		t.Run(stop, func(t *testing.T) {
			c, tr := r85toolsSetup(t, `{"resultType":"input_required","inputRequests":{"confirm":`+r85toolsElicitation+`}}`)
			entered := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			c.OnElicitationRequest(func(context.Context, ElicitationRequest) (ElicitResult, error) {
				close(entered)
				<-release
				return ElicitResult{Action: "accept", Content: map[string]interface{}{"ok": true}}, nil
			})
			ctx, cancel := context.WithCancel(WithRequestOptions(context.Background(), RequestOptions{MaxTotalTimeoutMS: 100}))
			defer cancel()
			if stop != "timeout" {
				ctx = WithRequestOptions(ctx, RequestOptions{MaxTotalTimeoutMS: 60_000})
			}
			errCh := make(chan error, 1)
			go func() {
				_, err := c.CallTool(ctx, "deploy", nil)
				errCh <- err
			}()
			<-entered
			switch stop {
			case "abort":
				cancel()
			case "close":
				_ = c.Close()
			}
			select {
			case err := <-errCh:
				want := map[string]string{"abort": "Request was aborted", "close": "Connection closed", "timeout": "Request timed out after 100ms"}[stop]
				// The timeout is the remaining total in whole ms (TS Date.now()).
				if err == nil || (err.Error() != want && (stop != "timeout" || !strings.HasPrefix(err.Error(), "Request timed out after 9"))) {
					t.Fatalf("err = %v, want %q", err, want)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("call did not stop")
			}
			if n := len(tr.recorded()); n != 1 {
				t.Fatalf("requests = %d, want 1", n)
			}
		})
	}
}

// Outside the modern loop (legacy era) input_required keeps failing with
// the new TS text.
func TestR85toolsInputRequiredLegacyText(t *testing.T) {
	c := NewMCPClient(newMockTransport(), MCPClientConfig{})
	c.protocolVersion = "2025-11-25"
	err := c.validateModernResult("tools/call", json.RawMessage(`{"resultType":"input_required"}`))
	if err == nil || err.Error() != "Input-required results are not supported for tools/call on protocol 2025-11-25" {
		t.Fatalf("err = %v", err)
	}
}

// Ports mcp-client.test.ts "should explain how to opt into legacy
// initialization" / "should preserve a version error with $name"
// (ade2a33f55).
func TestR85toolsDiscoveryLegacyVersionGuidance(t *testing.T) {
	connect := func(data string) (*r85toolsMRTRTransport, error) {
		errObj := `{"code":-32022,"message":"Unsupported protocol version"`
		if data != "" {
			errObj += `,"data":` + data
		}
		tr := &r85toolsMRTRTransport{messages: make(chan *MCPMessage, 8), results: []string{r85toolsComplete}, discoverErr: errObj + "}"}
		c := NewMCPClient(tr, MCPClientConfig{})
		err := c.Connect(context.Background())
		_ = c.Close()
		return tr, err
	}
	for _, supported := range []string{`["2025-11-25","2025-06-18"]`, `["2025-06-18"]`} {
		tr, err := connect(`{"requested":"2026-07-28","supported":` + supported + `}`)
		var mcpErr *MCPClientError
		if !errors.As(err, &mcpErr) || mcpErr.Code != -32022 {
			t.Fatalf("%s: err = %#v", supported, err)
		}
		var versions []string
		_ = json.Unmarshal([]byte(supported), &versions)
		want := "MCP server rejected protocol version 2026-07-28 during discovery. Server-supported versions: " + strings.Join(versions, ", ") + ". The client does not automatically switch to legacy initialization after this error. If this is a known legacy server, set protocolVersionDiscovery: false to use initialize."
		if mcpErr.Message != want {
			t.Fatalf("message = %q\nwant %q", mcpErr.Message, want)
		}
		var cause *MCPClientError
		if !errors.As(mcpErr.Cause, &cause) || cause.Message != "Unsupported protocol version" {
			t.Fatalf("cause = %#v", mcpErr.Cause)
		}
		if data, _ := mcpErr.Data.(map[string]interface{}); data["requested"] != "2026-07-28" {
			t.Fatalf("data = %#v", mcpErr.Data)
		}
		if !reflect.DeepEqual(tr.methods, []string{"server/discover"}) {
			t.Fatalf("sent = %v", tr.methods)
		}
	}
	for _, data := range []string{"", "null", "{}", `{"supported":"2025-06-18"}`, `{"supported":["2025-06-18",null]}`, `{"supported":[]}`, `{"supported":["2099-01-01"]}`, `{"supported":["2026-07-28","2025-06-18"]}`} {
		_, err := connect(data)
		var mcpErr *MCPClientError
		if !errors.As(err, &mcpErr) || mcpErr.Message != "Unsupported protocol version" {
			t.Fatalf("data %s: err = %v, want the original error", data, err)
		}
	}
}

// TS content-type check uses JS trim: a leading U+0085 is kept (415), a
// U+FEFF is trimmed (accepted).
func TestR85toolsWebhookContentTypeJSTrim(t *testing.T) {
	h := newWebhookHarness(t)
	r := signedEventRequest(h.sub, testEvent, "evt_1")
	r.Header.Set("content-type", "\u0085application/json")
	if code := h.do(r); code != 415 {
		t.Fatalf("NEL content type = %d, want 415", code)
	}
	r = signedEventRequest(h.sub, testEvent, "evt_1")
	r.Header.Set("content-type", "\ufeffapplication/json; charset=utf-8")
	if code := h.do(r); code == 415 {
		t.Fatalf("BOM content type = %d, want it accepted", code)
	}
}
