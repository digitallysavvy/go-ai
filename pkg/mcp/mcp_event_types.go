package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// This file ports the experimental MCP events types (TS mcp-event-types.ts,
// TS #22155, #22250, #22325). Events are experimental: the API may change in
// any release.

// Nullable represents an optional JSON value that distinguishes "not set"
// (omitted) from an explicit JSON null, matching the TypeScript
// `value?: T | null` shape.
//
// The zero value is "not set" and is omitted when marshaled with `omitzero`.
type Nullable[T any] struct {
	// Set is true when the field is present, even if Value is nil (null).
	Set bool
	// Value is the value, or nil for an explicit null.
	Value *T
}

// NullableOf returns a set Nullable holding v.
func NullableOf[T any](v T) Nullable[T] { return Nullable[T]{Set: true, Value: &v} }

// NullableNull returns a set Nullable holding an explicit null.
func NullableNull[T any]() Nullable[T] { return Nullable[T]{Set: true} }

// MarshalJSON implements json.Marshaler.
func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if n.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*n.Value)
}

// UnmarshalJSON implements json.Unmarshaler.
func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	n.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		n.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	n.Value = &v
	return nil
}

// MCP event delivery modes advertised by an event definition.
const (
	MCPEventDeliveryWebhook = "webhook"
	MCPEventDeliveryPoll    = "poll"
	MCPEventDeliveryPush    = "push"
)

// MCPEventDefinition describes an event type offered by an MCP server
// (TS MCPEventDefinition).
type MCPEventDefinition struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	// Delivery lists the supported delivery modes: "webhook", "poll", "push".
	Delivery []string `json:"delivery"`
	// InputSchema is a JSON Schema (object or boolean) for subscription arguments.
	InputSchema interface{} `json:"inputSchema"`
	// PayloadSchema is a JSON Schema (object or boolean) for event data.
	PayloadSchema interface{} `json:"payloadSchema"`
}

func (d MCPEventDefinition) validate() error {
	if d.Name == "" {
		return fmt.Errorf("invalid MCP event definition: name is required")
	}
	if len(d.Delivery) == 0 {
		return fmt.Errorf("invalid MCP event definition: delivery must contain at least one mode")
	}
	for _, mode := range d.Delivery {
		switch mode {
		case MCPEventDeliveryWebhook, MCPEventDeliveryPoll, MCPEventDeliveryPush:
		default:
			return fmt.Errorf("invalid MCP event definition: unknown delivery mode %q", mode)
		}
	}
	for _, schema := range []interface{}{d.InputSchema, d.PayloadSchema} {
		switch schema.(type) {
		case bool, map[string]interface{}:
		default:
			return fmt.Errorf("invalid MCP event definition: inputSchema and payloadSchema must be a boolean or an object")
		}
	}
	return nil
}

// ListEventsResult is the result of `events/list` (TS ListEventsResult).
type ListEventsResult struct {
	Events     []MCPEventDefinition `json:"events"`
	NextCursor *string              `json:"nextCursor,omitempty"`
}

// UnmarshalJSON validates the event definitions.
func (r *ListEventsResult) UnmarshalJSON(data []byte) error {
	type alias ListEventsResult
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	if a.Events == nil {
		return fmt.Errorf("invalid events/list result: events is required")
	}
	for _, event := range a.Events {
		if err := event.validate(); err != nil {
			return err
		}
	}
	*r = ListEventsResult(a)
	return nil
}

// SubscribeEventResult is the result of `events/subscribe`
// (TS SubscribeEventResult). It never contains the signing secret.
type SubscribeEventResult struct {
	ID string `json:"id"`
	// RefreshBefore is the RFC 3339 expiration, or nil when the subscription
	// does not expire.
	RefreshBefore *string `json:"refreshBefore"`
	// Cursor is nil when the server returned no cursor.
	Cursor    *string `json:"cursor"`
	Truncated bool    `json:"truncated"`
}

// UnmarshalJSON validates the result the way TS SubscribeEventResultSchema
// does: id is a non-empty string, refreshBefore is required (string or null),
// cursor is nullish and normalized to nil, truncated is a required boolean.
func (r *SubscribeEventResult) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var out SubscribeEventResult
	if err := json.Unmarshal(raw["id"], &out.ID); err != nil || out.ID == "" {
		return fmt.Errorf("invalid events/subscribe result: id must be a non-empty string")
	}
	rb, ok := raw["refreshBefore"]
	if !ok {
		return fmt.Errorf("invalid events/subscribe result: refreshBefore is required")
	}
	if err := json.Unmarshal(rb, &out.RefreshBefore); err != nil {
		return fmt.Errorf("invalid events/subscribe result: refreshBefore must be a string or null")
	}
	if out.RefreshBefore != nil && !isISODatetimeWithOffset(*out.RefreshBefore) {
		return fmt.Errorf("invalid events/subscribe result: refreshBefore must be an ISO datetime")
	}
	if c, ok := raw["cursor"]; ok {
		if err := json.Unmarshal(c, &out.Cursor); err != nil {
			return fmt.Errorf("invalid events/subscribe result: cursor must be a string")
		}
	}
	if err := json.Unmarshal(raw["truncated"], &out.Truncated); err != nil {
		return fmt.Errorf("invalid events/subscribe result: truncated must be a boolean")
	}
	*r = out
	return nil
}

func isISODatetimeWithOffset(s string) bool {
	_, ok := parseISODatetime(s)
	return ok
}

func parseISODatetime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// MCPEvent is an event envelope delivered to a webhook (TS MCPEvent).
type MCPEvent struct {
	EventID   string                 `json:"eventId"`
	Name      string                 `json:"name"`
	Timestamp string                 `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	// Cursor is the position after this event. HasCursor reports whether the
	// envelope carried a cursor field at all (it may be an explicit null).
	Cursor    *string `json:"cursor,omitempty"`
	HasCursor bool    `json:"-"`
}

// UnmarshalJSON validates the envelope the way TS MCPEventSchema does.
func (e *MCPEvent) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var out MCPEvent
	if err := json.Unmarshal(raw["eventId"], &out.EventID); err != nil || out.EventID == "" {
		return fmt.Errorf("invalid MCP event: eventId must be a non-empty string")
	}
	if err := json.Unmarshal(raw["name"], &out.Name); err != nil || out.Name == "" {
		return fmt.Errorf("invalid MCP event: name must be a non-empty string")
	}
	if err := json.Unmarshal(raw["timestamp"], &out.Timestamp); err != nil || !isISODatetimeWithOffset(out.Timestamp) {
		return fmt.Errorf("invalid MCP event: timestamp must be an ISO datetime")
	}
	if !bytes.HasPrefix(bytes.TrimSpace(raw["data"]), []byte("{")) {
		return fmt.Errorf("invalid MCP event: data must be an object")
	}
	if err := unmarshalSafeJSON(raw["data"], &out.Data); err != nil {
		return fmt.Errorf("invalid MCP event: data must be an object")
	}
	if c, ok := raw["cursor"]; ok {
		out.HasCursor = true
		if err := json.Unmarshal(c, &out.Cursor); err != nil {
			return fmt.Errorf("invalid MCP event: cursor must be a string")
		}
	}
	*e = out
	return nil
}

// MCPEventGap is a signed replay-gap control message: some events are no
// longer available (TS MCPEventControl with type "gap").
type MCPEventGap struct {
	Type   string `json:"type"`
	Cursor string `json:"cursor"`
	// Truncated is always true for a gap.
	Truncated bool `json:"truncated"`
}

// MCPEventTerminationError is the JSON-RPC style error in a termination.
type MCPEventTerminationError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCPEventTermination is a signed control message ending the subscription
// (TS MCPEventControl with type "terminated").
type MCPEventTermination struct {
	Type  string                   `json:"type"`
	Error MCPEventTerminationError `json:"error"`
}

// parseMCPEventControl validates a control message (an object with a `type`
// other than "verification"). Exactly one of gap/termination is non-nil on
// success.
func parseMCPEventControl(raw map[string]json.RawMessage) (*MCPEventGap, *MCPEventTermination, error) {
	var typ string
	if err := json.Unmarshal(raw["type"], &typ); err != nil {
		return nil, nil, fmt.Errorf("invalid MCP event control")
	}
	switch typ {
	case "gap":
		var cursor string
		if err := json.Unmarshal(raw["cursor"], &cursor); err != nil {
			return nil, nil, fmt.Errorf("invalid MCP event control: gap cursor must be a string")
		}
		return &MCPEventGap{Type: "gap", Cursor: cursor, Truncated: true}, nil, nil
	case "terminated":
		var errObj map[string]json.RawMessage
		if err := json.Unmarshal(raw["error"], &errObj); err != nil || errObj == nil {
			return nil, nil, fmt.Errorf("invalid MCP event control: terminated error must be an object")
		}
		var code json.Number
		dec := json.NewDecoder(bytes.NewReader(errObj["code"]))
		dec.UseNumber()
		if err := dec.Decode(&code); err != nil {
			return nil, nil, fmt.Errorf("invalid MCP event control: error code must be an integer")
		}
		codeInt, err := code.Int64()
		if err != nil {
			return nil, nil, fmt.Errorf("invalid MCP event control: error code must be an integer")
		}
		var message string
		if err := json.Unmarshal(errObj["message"], &message); err != nil {
			return nil, nil, fmt.Errorf("invalid MCP event control: error message must be a string")
		}
		term := &MCPEventTermination{Type: "terminated", Error: MCPEventTerminationError{Code: int(codeInt), Message: message}}
		if d, ok := errObj["data"]; ok {
			if err := json.Unmarshal(d, &term.Error.Data); err != nil {
				return nil, nil, err
			}
		}
		return nil, term, nil
	default:
		return nil, nil, fmt.Errorf("invalid MCP event control type %q", typ)
	}
}

// MCPEventSubscriptionStatus is the lifecycle state of a stored subscription.
type MCPEventSubscriptionStatus string

const (
	// MCPEventStatusPending means the subscribe/refresh response has not been
	// persisted yet; deliveries are retried (503) until it is active.
	MCPEventStatusPending MCPEventSubscriptionStatus = "pending"
	// MCPEventStatusActive means the subscription ID and expiration are saved.
	MCPEventStatusActive MCPEventSubscriptionStatus = "active"
)

// MCPEventDelivery is the webhook delivery configuration, including the
// signing secret. Persist it privately.
type MCPEventDelivery struct {
	Mode   string `json:"mode"`
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

// MCPEventDeliveryInfo is the delivery configuration without the secret.
type MCPEventDeliveryInfo struct {
	Mode string `json:"mode"`
	URL  string `json:"url"`
}

// MCPEventSubscription is a subscription record persisted privately by the
// application, including the signing secret (TS MCPEventSubscription).
type MCPEventSubscription struct {
	Key  string `json:"key"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	// Arguments are the subscription filter arguments.
	Arguments  map[string]interface{} `json:"arguments"`
	Definition MCPEventDefinition     `json:"definition"`
	Delivery   MCPEventDelivery       `json:"delivery"`
	// RefreshBefore is the RFC 3339 expiration; nil means no expiration.
	RefreshBefore *string `json:"refreshBefore,omitempty"`
	// Cursor is the last saved replay cursor (nil is JSON null).
	Cursor    *string `json:"cursor"`
	Truncated bool    `json:"truncated"`
	// TTLMs is the requested lifetime; an explicit null means non-expiring.
	TTLMs    Nullable[int64]            `json:"ttlMs,omitzero"`
	MaxAgeMs *int64                     `json:"maxAgeMs,omitempty"`
	Status   MCPEventSubscriptionStatus `json:"status"`
}

// MCPEventSubscriptionInfo is subscription metadata safe to pass to event
// handlers; it excludes the secret (TS MCPEventSubscriptionInfo).
type MCPEventSubscriptionInfo struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Arguments     map[string]interface{} `json:"arguments"`
	Delivery      MCPEventDeliveryInfo   `json:"delivery"`
	RefreshBefore *string                `json:"refreshBefore"`
}

// MCPEventSubscriptionPatch is a partial update of a stored subscription
// (TS Partial<MCPEventSubscription>). Only set fields are applied.
type MCPEventSubscriptionPatch struct {
	// ID, when Set, replaces the ID; a Set null clears it.
	ID            Nullable[string]
	RefreshBefore Nullable[string]
	Cursor        Nullable[string]
	Truncated     *bool
	Status        *MCPEventSubscriptionStatus
}

// ApplyTo merges the patch into s. Store implementations can use it to
// implement MCPEventStore.Update.
func (p MCPEventSubscriptionPatch) ApplyTo(s *MCPEventSubscription) {
	if p.ID.Set {
		s.ID = ""
		if p.ID.Value != nil {
			s.ID = *p.ID.Value
		}
	}
	if p.RefreshBefore.Set {
		s.RefreshBefore = copyStringPtr(p.RefreshBefore.Value)
	}
	if p.Cursor.Set {
		s.Cursor = copyStringPtr(p.Cursor.Value)
	}
	if p.Truncated != nil {
		s.Truncated = *p.Truncated
	}
	if p.Status != nil {
		s.Status = *p.Status
	}
}

func copyStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// MCPEventStore persists event subscriptions. Use durable, private storage
// scoped to one MCP server and authenticated principal. Update must atomically
// merge only the supplied fields. Never expose these records to a browser:
// Delivery.Secret authenticates webhooks.
//
// Get and GetByID return (nil, nil) when no record exists.
type MCPEventStore interface {
	Get(ctx context.Context, key string) (*MCPEventSubscription, error)
	GetByID(ctx context.Context, id string) (*MCPEventSubscription, error)
	Set(ctx context.Context, subscription MCPEventSubscription) error
	Update(ctx context.Context, key string, patch MCPEventSubscriptionPatch) error
	Delete(ctx context.Context, key string) error
}

// MCPEventsConfig configures experimental event support on MCPClientConfig.
// Set exactly one of Store (direct webhook subscriptions managed by this
// client) or Adapter (managed subscriptions owned by a backend).
type MCPEventsConfig struct {
	Store MCPEventStore
	// Adapter binds managed subscription operations to the transport.
	Adapter MCPEventAdapter
	// ValidateArguments optionally validates subscription arguments using an
	// application schema or the server's InputSchema. Return an error to
	// reject before persisting or subscribing. Only valid with Store.
	ValidateArguments func(ctx context.Context, definition MCPEventDefinition, arguments map[string]interface{}) error
}

// MCPEventWebhookDelivery configures webhook delivery for SubscribeEventOptions.
type MCPEventWebhookDelivery struct {
	// Mode must be empty or "webhook".
	Mode string
	// URL is the HTTPS callback served by ExperimentalCreateMCPEventWebhook
	// before subscribing.
	URL string
	// AllowInsecureLocalhost is development only: allow HTTP callbacks to
	// literal loopback addresses (127.0.0.1, ::1).
	AllowInsecureLocalhost bool
	// Secret is generated securely when empty and persisted before the
	// subscribe request.
	Secret string
}

// SubscribeEventOptions are the options for MCPEvents.Subscribe.
type SubscribeEventOptions struct {
	Name      string
	Arguments map[string]interface{}
	Delivery  MCPEventWebhookDelivery
	// Cursor resumes from a saved cursor; nil sends null.
	Cursor *string
	// TTLMs requests a lifetime; leave unset to omit, or set an explicit null
	// (NullableNull) to request a non-expiring subscription.
	TTLMs    Nullable[int64]
	MaxAgeMs *int64
}

// ListEventsOptions are the options for listing the event catalog.
type ListEventsOptions struct {
	// Cursor is the pagination cursor from a previous page.
	Cursor string
}

// MCPEventSubscriptionRef identifies a stored subscription by server ID or by
// local routing key. If ID is non-empty it is used, otherwise Key.
type MCPEventSubscriptionRef struct {
	ID  string
	Key string
}
