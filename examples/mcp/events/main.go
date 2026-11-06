// Command events subscribes to an MCP server's webhook events (experimental).
//
// It mirrors the TypeScript examples/mcp/src/events/client.ts: start the
// webhook receiver first (the server verifies the callback URL immediately),
// subscribe, trigger an event with a tool call, wait for the delivery, then
// unsubscribe.
//
// Requires an MCP server with the events capability at MCP_EVENTS_URL
// (default http://127.0.0.1:3002/mcp) exposing a publish_comment tool.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/mcp"
)

// memoryStore is single-process demo storage. Production stores must be
// durable and private: records contain webhook signing secrets.
type memoryStore struct {
	mu      sync.Mutex
	records map[string]mcp.MCPEventSubscription
}

func (s *memoryStore) Get(_ context.Context, key string) (*mcp.MCPEventSubscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.records[key]; ok {
		return &v, nil
	}
	return nil, nil
}

func (s *memoryStore) GetByID(_ context.Context, id string) (*mcp.MCPEventSubscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.records {
		if v.ID == id {
			return &v, nil
		}
	}
	return nil, nil
}

func (s *memoryStore) Set(_ context.Context, sub mcp.MCPEventSubscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[sub.Key] = sub
	return nil
}

func (s *memoryStore) Update(_ context.Context, key string, patch mcp.MCPEventSubscriptionPatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.records[key]
	if !ok {
		return fmt.Errorf("unknown subscription %q", key)
	}
	patch.ApplyTo(&v)
	s.records[key] = v
	return nil
}

func (s *memoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, key)
	return nil
}

// bearerTransport adds a static bearer token to every MCP request.
type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(req)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store := &memoryStore{records: map[string]mcp.MCPEventSubscription{}}
	received := make(chan struct{}, 1)

	// Uses the saved secret to verify callback challenges and signatures.
	webhook, err := mcp.ExperimentalCreateMCPEventWebhook(mcp.MCPEventWebhookOptions{
		Store: store,
		OnEvent: func(_ context.Context, args mcp.MCPEventWebhookEventArgs) error {
			fmt.Println("Received comment:", args.Event.Data)
			select {
			case received <- struct{}{}:
			default:
			}
			return nil
		},
		OnGap: func(_ context.Context, args mcp.MCPEventWebhookGapArgs) error {
			// Some events cannot be replayed; reconcile state or notify the agent.
			fmt.Println("Replay gap:", args.Subscription.ID)
			return nil
		},
		OnTerminated: func(_ context.Context, args mcp.MCPEventWebhookTerminatedArgs) error {
			fmt.Println("Subscription terminated:", args.Subscription.ID, args.Termination.Error.Message)
			return nil
		},
		OnError: func(err error) { log.Println("webhook error:", err) },
	})
	if err != nil {
		return err
	}

	// Start receiving before subscribing: the server verifies this URL immediately.
	mux := http.NewServeMux()
	mux.Handle("/mcp-events", webhook)
	listener, err := net.Listen("tcp", "127.0.0.1:3003")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Shutdown(context.Background()) }()

	serverURL := os.Getenv("MCP_EVENTS_URL")
	if serverURL == "" {
		serverURL = "http://127.0.0.1:3002/mcp"
	}
	token := os.Getenv("MCP_EVENTS_TOKEN")
	if token == "" {
		token = "local-events-demo"
	}
	transport := mcp.NewHTTPTransport(mcp.HTTPTransportConfig{
		URL:        serverURL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: token}},
	})
	client := mcp.NewMCPClient(transport, mcp.MCPClientConfig{
		ExperimentalEvents: &mcp.MCPEventsConfig{Store: store},
	})
	if err := client.Connect(ctx); err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck

	events := client.ExperimentalEvents()
	catalog, err := events.List(ctx, mcp.ListEventsOptions{})
	if err != nil {
		return err
	}
	for _, e := range catalog.Events {
		fmt.Println("Available event:", e.Name)
	}

	subscription, err := events.Subscribe(ctx, mcp.SubscribeEventOptions{
		Name:      "comment.created",
		Arguments: map[string]interface{}{"document_id": "doc_123"},
		Delivery: mcp.MCPEventWebhookDelivery{
			URL:                    "http://127.0.0.1:3003/mcp-events",
			AllowInsecureLocalhost: true, // Local demo; production uses HTTPS.
		},
	})
	if err != nil {
		return err
	}
	fmt.Println("Subscribed (callback verified):", subscription.ID)
	defer func() {
		_ = events.Unsubscribe(context.Background(), mcp.MCPEventSubscriptionRef{ID: subscription.ID})
	}()

	// A tool response does not generally mean its webhook has arrived.
	result, err := client.CallTool(ctx, "publish_comment", map[string]interface{}{
		"document_id": "doc_123",
		"text":        "Hello from MCP events!",
	})
	if err != nil {
		return err
	}
	if result.IsError {
		return fmt.Errorf("failed to publish comment")
	}
	select {
	case <-received:
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("no event received within 10 seconds")
	}
}
