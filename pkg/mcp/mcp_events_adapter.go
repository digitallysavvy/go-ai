package mcp

import "context"

// ManagedSubscriptionStatus is the state of a managed subscription.
type ManagedSubscriptionStatus string

// Managed subscription states.
const (
	ManagedSubscriptionPending   ManagedSubscriptionStatus = "pending"
	ManagedSubscriptionActive    ManagedSubscriptionStatus = "active"
	ManagedSubscriptionNeedsAuth ManagedSubscriptionStatus = "needs_auth"
	ManagedSubscriptionStopped   ManagedSubscriptionStatus = "stopped"
	ManagedSubscriptionExpired   ManagedSubscriptionStatus = "expired"
	ManagedSubscriptionFailed    ManagedSubscriptionStatus = "failed"
)

// ManagedSubscription is a durable watch owned by the adapter's backend, not
// an upstream MCP lease (TS ManagedSubscription).
type ManagedSubscription struct {
	// ID is the backend subscription ID; use it for managed lifecycle operations.
	ID        string                    `json:"id"`
	Name      string                    `json:"name"`
	Arguments map[string]interface{}    `json:"arguments"`
	Status    ManagedSubscriptionStatus `json:"status"`
	// ExpiresAt is the application monitoring deadline; nil means until
	// explicitly stopped.
	ExpiresAt *string `json:"expiresAt"`
	// CleanupStatus is "pending", "complete" or "exhausted" when reported.
	CleanupStatus string `json:"cleanupStatus,omitempty"`
}

// ManagedSubscribeInput is the input to ManagedMCPEvents.Subscribe
// (TS ManagedSubscribeInput). TTLMs is required: set a duration or an
// explicit null (NullableNull) to watch until stopped. expiresAt is a response
// field only (ManagedSubscription.ExpiresAt), not a creation input.
type ManagedSubscribeInput struct {
	Name      string
	Arguments map[string]interface{}
	// Context is application metadata, not an authorization grant.
	Context map[string]interface{}
	// IdempotencyKey is reused to recover an uncertain create result.
	IdempotencyKey string
	// TTLMs is the application lifetime in milliseconds, or an explicit null
	// (NullableNull) to watch until explicitly stopped.
	TTLMs Nullable[int64]
}

// ListManagedSubscriptionsInput filters ManagedMCPEvents.ListSubscriptions.
type ListManagedSubscriptionsInput struct {
	Cursor string
	Limit  int
	Status ManagedSubscriptionStatus
}

// ListManagedSubscriptionsResult is one page of managed subscriptions.
type ListManagedSubscriptionsResult struct {
	Subscriptions []ManagedSubscription
	NextCursor    string
}

// MCPEventOperations is bound to an authorized account and destination by the
// application. The backend owns callback provisioning, verification,
// persistence, renewal and cleanup. Implementations must honor ctx and
// preserve actionable errors. Cancelling a request does not cancel an already
// accepted remote subscription.
type MCPEventOperations interface {
	Subscribe(ctx context.Context, input ManagedSubscribeInput) (*ManagedSubscription, error)
	GetSubscription(ctx context.Context, id string) (*ManagedSubscription, error)
	ListSubscriptions(ctx context.Context, input ListManagedSubscriptionsInput) (*ListManagedSubscriptionsResult, error)
	Unsubscribe(ctx context.Context, id string) (*ManagedSubscription, error)
}

// MCP event adapter transport types.
const (
	MCPEventTransportHTTP   = "http"
	MCPEventTransportSSE    = "sse"
	MCPEventTransportCustom = "custom"
)

// MCPEventAdapterTransport is transport metadata passed to CreateAdapter.
// URL is the configured endpoint (not a resolved redirect target) and is
// empty for custom transports, which have no standard URL.
type MCPEventAdapterTransport struct {
	// Type is "http", "sse" or "custom".
	Type string
	URL  string
}

// MCPEventAdapterContext is the context passed to MCPEventAdapter.CreateAdapter.
type MCPEventAdapterContext struct {
	Transport MCPEventAdapterTransport
}

// MCPEventAdapter creates bound subscription operations once per client,
// before the transport starts. It receives transport metadata, not
// credentials or a live transport instance.
type MCPEventAdapter interface {
	CreateAdapter(context MCPEventAdapterContext) (MCPEventOperations, error)
}

// ManagedMCPEvents is the event surface of a client configured with an
// adapter: catalog discovery uses MCP; all subscription operations use the
// adapter (TS ManagedMCPEvents).
type ManagedMCPEvents interface {
	MCPEventOperations
	List(ctx context.Context, options ListEventsOptions) (*ListEventsResult, error)
}

type managedMCPEvents struct {
	operations MCPEventOperations
	events     *MCPEvents
}

func (m *managedMCPEvents) List(ctx context.Context, options ListEventsOptions) (*ListEventsResult, error) {
	return m.events.List(ctx, options)
}

func (m *managedMCPEvents) Subscribe(ctx context.Context, input ManagedSubscribeInput) (*ManagedSubscription, error) {
	return m.operations.Subscribe(ctx, input)
}

func (m *managedMCPEvents) GetSubscription(ctx context.Context, id string) (*ManagedSubscription, error) {
	return m.operations.GetSubscription(ctx, id)
}

func (m *managedMCPEvents) ListSubscriptions(ctx context.Context, input ListManagedSubscriptionsInput) (*ListManagedSubscriptionsResult, error) {
	return m.operations.ListSubscriptions(ctx, input)
}

func (m *managedMCPEvents) Unsubscribe(ctx context.Context, id string) (*ManagedSubscription, error) {
	return m.operations.Unsubscribe(ctx, id)
}

func newMCPEventOperationsError() error {
	return NewMCPClientError(0, "experimental_events.adapter.CreateAdapter() must return an object implementing Subscribe, GetSubscription, ListSubscriptions, and Unsubscribe.", nil)
}
