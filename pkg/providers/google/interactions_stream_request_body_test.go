package google

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestInteractionsEventStream_ImplementsStreamRequestBody verifies the
// non-agent (plain SSE) DoStream path exposes the raw request body it sent
// via the optional provider.StreamRequestBody capability (hand-off: "stream
// request body field").
func TestInteractionsEventStream_ImplementsStreamRequestBody(t *testing.T) {
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `{"event_type":"interaction.completed","interaction":{"id":"v1","status":"completed","usage":{"total_tokens":1}}}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewInteractionsLanguageModel(p, ModelGemini25Flash)

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	rb, ok := stream.(provider.StreamRequestBody)
	if !ok {
		t.Fatalf("stream (%T) does not implement provider.StreamRequestBody", stream)
	}
	if rb.RequestBody() == nil {
		t.Fatal("RequestBody() = nil, want the sent request body")
	}
	if capturedBody == nil {
		t.Fatal("server never received a request body")
	}
}

// TestInteractionsPollingStream_ImplementsStreamRequestBody verifies the
// agent (background POST + GET-stream polling) DoStream path exposes the
// raw request body via provider.StreamRequestBody.
func TestInteractionsPollingStream_ImplementsStreamRequestBody(t *testing.T) {
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/interactions":
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			_, _ = fmt.Fprint(w, `{"id":"v1_poll","status":"in_progress"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/interactions/v1_poll":
			w.Header().Set("Content-Type", "text/event-stream")
			writeSSE(w, `{"event_type":"interaction.completed","interaction":{"id":"v1_poll","status":"completed","usage":{"total_tokens":1}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.InteractionsAgent(InteractionsAgentDeepResearch)
	if err != nil {
		t.Fatalf("InteractionsAgent: %v", err)
	}

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	rb, ok := stream.(provider.StreamRequestBody)
	if !ok {
		t.Fatalf("stream (%T) does not implement provider.StreamRequestBody", stream)
	}
	if rb.RequestBody() == nil {
		t.Fatal("RequestBody() = nil, want the sent request body")
	}
	if capturedBody == nil {
		t.Fatal("server never received a request body")
	}
}

// TestInteractionsSynthesizedStream_ImplementsStreamRequestBody verifies the
// agent path's synthesized (already-terminal) stream exposes the raw
// request body via provider.StreamRequestBody.
func TestInteractionsSynthesizedStream_ImplementsStreamRequestBody(t *testing.T) {
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"v1_term","status":"completed","usage":{"total_tokens":1}}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.InteractionsAgent(InteractionsAgentDeepResearch)
	if err != nil {
		t.Fatalf("InteractionsAgent: %v", err)
	}

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	rb, ok := stream.(provider.StreamRequestBody)
	if !ok {
		t.Fatalf("stream (%T) does not implement provider.StreamRequestBody", stream)
	}
	if rb.RequestBody() == nil {
		t.Fatal("RequestBody() = nil, want the sent request body")
	}
	if capturedBody == nil {
		t.Fatal("server never received a request body")
	}
}
