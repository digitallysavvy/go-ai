package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const mcpEventMaxBodyBytes = 256 * 1024

// MCPEventWebhookEventArgs is passed to MCPEventWebhookOptions.OnEvent.
type MCPEventWebhookEventArgs struct {
	Subscription MCPEventSubscriptionInfo
	Event        MCPEvent
}

// MCPEventWebhookGapArgs is passed to MCPEventWebhookOptions.OnGap.
type MCPEventWebhookGapArgs struct {
	Subscription MCPEventSubscriptionInfo
	MessageID    string
	Gap          MCPEventGap
}

// MCPEventWebhookTerminatedArgs is passed to MCPEventWebhookOptions.OnTerminated.
type MCPEventWebhookTerminatedArgs struct {
	Subscription MCPEventSubscriptionInfo
	MessageID    string
	Termination  MCPEventTermination
}

// MCPEventWebhookOptions configures ExperimentalCreateMCPEventWebhook
// (TS MCPEventWebhookOptions).
type MCPEventWebhookOptions struct {
	// Store is the same store the client uses to subscribe. Required.
	Store MCPEventStore

	// ValidatePayload optionally validates event data using an application
	// schema or the server's PayloadSchema. Return an error to reject the
	// payload with 400 before OnEvent runs. The event envelope and signature
	// are always checked.
	ValidatePayload func(ctx context.Context, definition MCPEventDefinition, data map[string]interface{}) error

	// OnEvent must return only after durable acceptance or successful
	// processing. Deliveries may be duplicated or reordered; deduplicate by
	// subscription ID + event ID. Return an error to respond 503 so the server
	// can retry. The helper does not run agents. Required.
	OnEvent func(ctx context.Context, args MCPEventWebhookEventArgs) error

	// OnGap handles a signed replay gap: some events are no longer available.
	// Reconcile application state or notify the agent, then return after
	// durable acceptance. The helper saves the fresh cursor and truncated
	// flag; delivery continues. Return an error to respond 503 without
	// changing saved state. Deduplicate by subscription ID + MessageID.
	OnGap func(ctx context.Context, args MCPEventWebhookGapArgs) error

	// OnTerminated handles signed termination: the server has ended the
	// subscription. The helper removes the saved subscription after this
	// callback succeeds. Return an error to respond 503 without changing saved
	// state. Deduplicate by subscription ID + MessageID.
	OnTerminated func(ctx context.Context, args MCPEventWebhookTerminatedArgs) error

	// OnError receives errors that caused a 503 response (or a rejected
	// payload validation).
	OnError func(err error)
}

type mcpEventWebhook struct {
	opts MCPEventWebhookOptions
}

// ExperimentalCreateMCPEventWebhook creates an http.Handler for MCP webhook
// deliveries. Mount it at the callback URL before subscribing. Verification
// is enabled automatically using the pending delivery secret saved by the
// client's Subscribe method.
//
// This API is experimental and may change in any release.
func ExperimentalCreateMCPEventWebhook(opts MCPEventWebhookOptions) (http.Handler, error) {
	if opts.Store == nil {
		return nil, NewMCPClientError(0, "MCP event webhook requires a Store", nil)
	}
	if opts.OnEvent == nil {
		return nil, NewMCPClientError(0, "MCP event webhook requires an OnEvent handler", nil)
	}
	return &mcpEventWebhook{opts: opts}, nil
}

func (h *mcpEventWebhook) onError(err error) {
	if h.opts.OnError != nil {
		h.opts.OnError(err)
	}
}

func (h *mcpEventWebhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	status, body := h.handle(w, r)
	if body != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
		return
	}
	w.WriteHeader(status)
}

func (h *mcpEventWebhook) handle(w http.ResponseWriter, r *http.Request) (int, []byte) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		return http.StatusMethodNotAllowed, nil
	}
	if strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])) != "application/json" {
		return http.StatusUnsupportedMediaType, nil
	}
	if r.ContentLength > mcpEventMaxBodyBytes {
		return http.StatusRequestEntityTooLarge, nil
	}

	ctx := r.Context()
	key := r.URL.Query().Get(MCPEventCallbackKey)
	id := r.Header.Get("X-MCP-Subscription-Id")
	if key == "" || id == "" {
		return http.StatusUnauthorized, nil
	}
	sub, err := h.opts.Store.Get(ctx, key)
	if err != nil {
		h.onError(err)
		return http.StatusServiceUnavailable, nil
	}
	if sub == nil || (sub.ID != "" && sub.ID != id) {
		return http.StatusUnauthorized, nil
	}

	// Bound streamed bodies too; Content-Length is optional and untrusted.
	if r.Body == nil {
		return http.StatusBadRequest, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, mcpEventMaxBodyBytes+1))
	if err != nil {
		h.onError(err)
		return http.StatusServiceUnavailable, nil
	}
	if len(body) > mcpEventMaxBodyBytes {
		return http.StatusRequestEntityTooLarge, nil
	}

	verified, err := verifyMCPEventSignature(r.Header, body, sub.Delivery.Secret)
	if err != nil {
		h.onError(err)
		return http.StatusServiceUnavailable, nil
	}
	if !verified {
		return http.StatusUnauthorized, nil
	}

	var value interface{}
	if err := unmarshalSafeJSON(body, &value); err != nil || len(strings.TrimSpace(string(body))) == 0 {
		return http.StatusBadRequest, nil
	}

	var gap *MCPEventGap
	var termination *MCPEventTermination
	if obj, ok := value.(map[string]interface{}); ok {
		if typ, hasType := obj["type"]; hasType {
			var rawObj map[string]json.RawMessage
			if err := json.Unmarshal(body, &rawObj); err != nil {
				return http.StatusBadRequest, nil
			}
			if typ == "verification" {
				var challenge string
				if err := json.Unmarshal(rawObj["challenge"], &challenge); err != nil || challenge == "" {
					return http.StatusBadRequest, nil
				}
				resp, _ := json.Marshal(map[string]string{"challenge": challenge})
				return http.StatusOK, resp
			}
			gap, termination, err = parseMCPEventControl(rawObj)
			if err != nil {
				return http.StatusBadRequest, nil
			}
		}
	}
	isControl := gap != nil || termination != nil

	// Servers can POST before a subscribe or refresh response is persisted.
	// Events and gaps must retry until the ID and renewed expiration are saved.
	// A verified termination for a known ID can end a pending renewal too:
	// access may have been revoked, so renewal can no longer succeed.
	if sub.ID == "" || (sub.Status != MCPEventStatusActive && termination == nil) {
		return http.StatusServiceUnavailable, nil
	}
	info := MCPEventSubscriptionInfo{
		ID:            sub.ID,
		Name:          sub.Name,
		Arguments:     sub.Arguments,
		Delivery:      MCPEventDeliveryInfo{Mode: MCPEventDeliveryWebhook, URL: sub.Delivery.URL},
		RefreshBefore: sub.RefreshBefore,
	}

	if isControl {
		messageID := r.Header.Get("webhook-id")
		if gap != nil {
			if h.opts.OnGap != nil {
				if err := h.opts.OnGap(ctx, MCPEventWebhookGapArgs{Subscription: info, MessageID: messageID, Gap: *gap}); err != nil {
					h.onError(err)
					return http.StatusServiceUnavailable, nil
				}
			}
			truncated := true
			if err := h.opts.Store.Update(ctx, sub.Key, MCPEventSubscriptionPatch{
				Cursor:    NullableOf(gap.Cursor),
				Truncated: &truncated,
			}); err != nil {
				h.onError(err)
				return http.StatusServiceUnavailable, nil
			}
		} else {
			if h.opts.OnTerminated != nil {
				if err := h.opts.OnTerminated(ctx, MCPEventWebhookTerminatedArgs{Subscription: info, MessageID: messageID, Termination: *termination}); err != nil {
					h.onError(err)
					return http.StatusServiceUnavailable, nil
				}
			}
			if err := h.opts.Store.Delete(ctx, sub.Key); err != nil {
				h.onError(err)
				return http.StatusServiceUnavailable, nil
			}
		}
		return http.StatusNoContent, nil
	}

	if sub.RefreshBefore != nil {
		if t, ok := parseISODatetime(*sub.RefreshBefore); ok && !t.After(mcpEventNow()) {
			return http.StatusGone, nil
		}
	}
	var event MCPEvent
	if err := json.Unmarshal(body, &event); err != nil ||
		event.Name != sub.Name ||
		event.EventID != r.Header.Get("webhook-id") {
		return http.StatusBadRequest, nil
	}
	if h.opts.ValidatePayload != nil {
		if err := h.opts.ValidatePayload(ctx, sub.Definition, event.Data); err != nil {
			h.onError(err)
			return http.StatusBadRequest, nil
		}
	}
	if err := h.opts.OnEvent(ctx, MCPEventWebhookEventArgs{Subscription: info, Event: event}); err != nil {
		h.onError(err)
		return http.StatusServiceUnavailable, nil
	}
	if event.HasCursor {
		if err := h.opts.Store.Update(ctx, sub.Key, MCPEventSubscriptionPatch{
			Cursor: Nullable[string]{Set: true, Value: event.Cursor},
		}); err != nil {
			h.onError(err)
			return http.StatusServiceUnavailable, nil
		}
	}
	return http.StatusNoContent, nil
}
