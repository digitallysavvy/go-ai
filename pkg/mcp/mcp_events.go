package mcp

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/urlnorm"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// MCPEventCallbackKey is the query parameter that routes a webhook delivery
// to its stored subscription (TS MCP_EVENT_CALLBACK_KEY).
const MCPEventCallbackKey = "mcp_event_subscription"

// MCPEvents is the experimental event discovery and webhook subscription
// lifecycle of a client configured with MCPEventsConfig.Store. Obtain it with
// (*MCPClient).ExperimentalEvents.
type MCPEvents struct {
	call              func(ctx context.Context, method string, params interface{}, result interface{}) error
	store             MCPEventStore
	validateArguments func(ctx context.Context, definition MCPEventDefinition, arguments map[string]interface{}) error
}

func (e *MCPEvents) getStore() (MCPEventStore, error) {
	if e.store == nil {
		return nil, NewMCPClientError(0, "Configure experimental_events.store to manage direct event subscriptions", nil)
	}
	return e.store, nil
}

func (e *MCPEvents) getSubscription(ctx context.Context, ref MCPEventSubscriptionRef) (*MCPEventSubscription, error) {
	store, err := e.getStore()
	if err != nil {
		return nil, err
	}
	var sub *MCPEventSubscription
	label := ref.Key
	if ref.ID != "" {
		label = ref.ID
		sub, err = store.GetByID(ctx, ref.ID)
	} else {
		sub, err = store.Get(ctx, ref.Key)
	}
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, NewMCPClientError(0, fmt.Sprintf("Unknown MCP event subscription: %s", label), nil)
	}
	return sub, nil
}

func (e *MCPEvents) subscribe(ctx context.Context, sub MCPEventSubscription) (*SubscribeEventResult, error) {
	// TS params {name, arguments, delivery, cursor, ttlMs?, maxAgeMs?} in
	// that key order.
	params := providerutils.OrderedMap{
		Keys: []string{"name", "arguments", "delivery", "cursor", "ttlMs", "maxAgeMs"},
		Vals: map[string]interface{}{
			"name":      sub.Name,
			"arguments": sub.Arguments,
			"delivery":  sub.Delivery,
			"cursor":    sub.Cursor,
		},
	}
	if sub.TTLMs.Set {
		params.Vals["ttlMs"] = sub.TTLMs
	}
	if sub.MaxAgeMs != nil {
		params.Vals["maxAgeMs"] = *sub.MaxAgeMs
	}
	var result SubscribeEventResult
	if err := e.call(ctx, "events/subscribe", params, &result); err != nil {
		return nil, err
	}
	store, err := e.getStore()
	if err != nil {
		return nil, err
	}
	active := MCPEventStatusActive
	truncated := result.Truncated
	patch := MCPEventSubscriptionPatch{
		ID:        NullableOf(result.ID),
		Cursor:    Nullable[string]{Set: true, Value: result.Cursor},
		Truncated: &truncated,
		Status:    &active,
	}
	patch.RefreshBefore = Nullable[string]{Set: true, Value: result.RefreshBefore}
	if err := store.Update(ctx, sub.Key, patch); err != nil {
		return nil, err
	}
	return &result, nil
}

// List lists the event catalog from the server (`events/list`). It does not
// require a store.
func (e *MCPEvents) List(ctx context.Context, options ListEventsOptions) (*ListEventsResult, error) {
	var cursor *string
	if options.Cursor != "" {
		cursor = &options.Cursor
	}
	return e.listPage(ctx, cursor)
}

// listPage lists one page; a non-nil cursor, even "", is sent (TS
// params: { cursor: pageCursor } with pageCursor !== undefined).
func (e *MCPEvents) listPage(ctx context.Context, cursor *string) (*ListEventsResult, error) {
	params := map[string]interface{}{}
	if cursor != nil {
		params["cursor"] = *cursor
	}
	var result ListEventsResult
	if err := e.call(ctx, "events/list", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Subscribe registers a webhook subscription. The callback URL must be served
// by ExperimentalCreateMCPEventWebhook before subscribing. The pending
// record, including the signing secret, is persisted before the subscribe
// request so signed verification can be answered immediately. Pending state is
// retained on errors: a timed-out request may have registered the callback
// remotely, so recover or unsubscribe using the stored key.
func (e *MCPEvents) Subscribe(ctx context.Context, options SubscribeEventOptions) (*SubscribeEventResult, error) {
	store, err := e.getStore()
	if err != nil {
		return nil, err
	}
	// TS `new URL(delivery.url)` (WHATWG): an unparseable URL is its
	// TypeError "Invalid URL"; the checks read url.protocol, url.hostname,
	// url.username / url.password and url.hash ("" for an empty fragment),
	// and the stored callback URL is url.href.
	u, err := urlnorm.Parse(options.Delivery.URL)
	if err != nil {
		return nil, urlnorm.ErrInvalidURL
	}
	localHTTP := options.Delivery.AllowInsecureLocalhost &&
		u.Scheme == "http" &&
		(u.Host == "127.0.0.1" || u.Host == "[::1]")
	if (u.Scheme != "https" && !localHTTP) || u.Username != "" || u.Password != "" || (u.Fragment != nil && *u.Fragment != "") {
		return nil, NewMCPClientError(0, "MCP event callback URLs must use HTTPS without credentials or fragments", nil)
	}
	if options.Delivery.Mode != "" && options.Delivery.Mode != MCPEventDeliveryWebhook {
		return nil, NewMCPClientError(0, "Only webhook event delivery is supported", nil)
	}
	if (options.TTLMs.Value != nil && *options.TTLMs.Value <= 0) ||
		(options.MaxAgeMs != nil && *options.MaxAgeMs < 0) {
		return nil, NewMCPClientError(0, "Invalid MCP event subscription lifetime or replay age", nil)
	}

	args := options.Arguments
	if args == nil {
		args = map[string]interface{}{}
	}

	var definition *MCPEventDefinition
	var pageCursor *string
	visited := map[string]bool{}
	for {
		page, err := e.listPage(ctx, pageCursor)
		if err != nil {
			return nil, err
		}
		for i := range page.Events {
			if page.Events[i].Name == options.Name {
				definition = &page.Events[i]
				break
			}
		}
		if definition != nil {
			break
		}
		// TS loops while nextCursor != null: an empty-string cursor continues.
		pageCursor = page.NextCursor
		if pageCursor == nil {
			break
		}
		if visited[*pageCursor] {
			return nil, NewMCPClientError(0, "Repeated MCP events pagination cursor", nil)
		}
		visited[*pageCursor] = true
	}
	if definition == nil {
		return nil, NewMCPClientError(0, fmt.Sprintf("Unknown MCP event: %s", options.Name), nil)
	}
	supportsWebhook := false
	for _, mode := range definition.Delivery {
		if mode == MCPEventDeliveryWebhook {
			supportsWebhook = true
		}
	}
	if !supportsWebhook {
		return nil, NewMCPClientError(0, fmt.Sprintf("MCP event %s does not support webhook delivery", options.Name), nil)
	}
	if e.validateArguments != nil {
		if err := e.validateArguments(ctx, *definition, args); err != nil {
			return nil, err
		}
	}
	secret := options.Delivery.Secret
	if secret == "" {
		secret, err = createMCPEventSecret()
		if err != nil {
			return nil, err
		}
	}
	if _, err := decodeMCPEventSecret(secret); err != nil {
		return nil, err
	}

	// A routing key is available before the server-derived subscription ID.
	// Persist it and the secret before the server attempts callback verification.
	key, err := newMCPEventKey()
	if err != nil {
		return nil, err
	}
	rawQuery := ""
	if u.Query != nil {
		rawQuery = *u.Query
	}
	query := appendMCPEventQuery(rawQuery, MCPEventCallbackKey, key)
	u.Query = &query
	sub := MCPEventSubscription{
		Key:        key,
		Name:       options.Name,
		Arguments:  cloneJSONMap(args),
		Definition: *definition,
		Delivery:   MCPEventDelivery{Mode: MCPEventDeliveryWebhook, URL: u.String(), Secret: secret},
		Cursor:     copyStringPtr(options.Cursor),
		TTLMs:      options.TTLMs,
		MaxAgeMs:   options.MaxAgeMs,
		Status:     MCPEventStatusPending,
	}
	if err := store.Set(ctx, sub); err != nil {
		return nil, err
	}
	return e.subscribe(ctx, sub)
}

// Refresh renews a stored subscription. Deliveries are retried (503) until
// the renewed expiration is persisted; pending state is retained on errors
// because the server may have renewed already.
func (e *MCPEvents) Refresh(ctx context.Context, ref MCPEventSubscriptionRef) (*SubscribeEventResult, error) {
	sub, err := e.getSubscription(ctx, ref)
	if err != nil {
		return nil, err
	}
	store, _ := e.getStore()
	pending := MCPEventStatusPending
	if err := store.Update(ctx, sub.Key, MCPEventSubscriptionPatch{Status: &pending}); err != nil {
		return nil, err
	}
	sub.Status = pending
	return e.subscribe(ctx, *sub)
}

// Unsubscribe ends a stored subscription and removes the persisted record
// only after the server request succeeds.
func (e *MCPEvents) Unsubscribe(ctx context.Context, ref MCPEventSubscriptionRef) error {
	sub, err := e.getSubscription(ctx, ref)
	if err != nil {
		return err
	}
	params := providerutils.OrderedMap{
		Keys: []string{"name", "arguments", "delivery"},
		Vals: map[string]interface{}{
			"name":      sub.Name,
			"arguments": sub.Arguments,
			"delivery":  MCPEventDeliveryInfo{Mode: MCPEventDeliveryWebhook, URL: sub.Delivery.URL},
		},
	}
	if err := e.call(ctx, "events/unsubscribe", params, nil); err != nil {
		return err
	}
	store, _ := e.getStore()
	return store.Delete(ctx, sub.Key)
}

// appendMCPEventQuery is TS url.searchParams.set(name, value): the first
// parameter of that name is replaced in place (later ones dropped), or the
// pair is appended, and the whole query is re-serialized with the
// URLSearchParams form encoding (so "a%20b" becomes "a+b" and "~" "%7E").
func appendMCPEventQuery(rawQuery, name, value string) string {
	q := parseOrderedQuery(rawQuery)
	q.set(name, value)
	return q.encode()
}

func newMCPEventKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func cloneJSONMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	data, err := json.Marshal(in)
	if err != nil {
		return in
	}
	var out map[string]interface{}
	if err := unmarshalSafeJSON(data, &out); err != nil {
		return in
	}
	return out
}
