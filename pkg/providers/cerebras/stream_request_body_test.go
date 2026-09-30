package cerebras

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestDoStream_ImplementsStreamRequestBody verifies that the TextStream
// returned by DoStream exposes the raw request body it sent via the
// optional provider.StreamRequestBody capability (hand-off: "stream request
// body field"), both for the plain pass-through path and for the
// cerebrasStream JSON-mode wrapper (which must delegate to the base
// OpenAI-compatible stream).
func TestDoStream_ImplementsStreamRequestBody(t *testing.T) {
	for _, tc := range []struct {
		name           string
		responseFormat *provider.ResponseFormat
	}{
		{name: "plain"},
		{name: "json_mode", responseFormat: &provider.ResponseFormat{Type: "json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var capturedBody map[string]interface{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
				_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
			}))
			defer server.Close()

			model, err := New(Config{APIKey: "test-key", BaseURL: server.URL}).LanguageModel("llama3.1-8b")
			if err != nil {
				t.Fatalf("LanguageModel error = %v", err)
			}

			stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
				Prompt:         types.Prompt{Text: "hi"},
				ResponseFormat: tc.responseFormat,
			})
			if err != nil {
				t.Fatalf("DoStream() error = %v", err)
			}
			defer stream.Close() //nolint:errcheck

			rb, ok := stream.(provider.StreamRequestBody)
			if !ok {
				t.Fatalf("stream (%T) does not implement provider.StreamRequestBody", stream)
			}
			body, ok := rb.RequestBody().(map[string]interface{})
			if !ok {
				t.Fatalf("RequestBody() = %#v, want a map[string]interface{}", rb.RequestBody())
			}
			if body["stream"] != true {
				t.Errorf("RequestBody()[\"stream\"] = %v, want true", body["stream"])
			}
			if capturedBody == nil {
				t.Fatal("server never received a request body")
			}
			if body["model"] != capturedBody["model"] {
				t.Errorf("RequestBody()[\"model\"] = %v, want %v (sent JSON)", body["model"], capturedBody["model"])
			}
		})
	}
}

// TestDoStream_RequestBodyReflectsPostTransform verifies that RequestBody()
// exposes the request body *after* Cerebras's structural rewrite (TS
// transformCerebrasRequestBody: max_tokens -> max_completion_tokens) rather
// than the pre-transform OpenAI-compatible body. Before this hook was wired
// as openai.Config.TransformRequestBody, the rewrite only happened inside
// cerebrasTransformTransport at the HTTP RoundTripper layer, which runs
// after DoStream already captured and exposed the pre-rewrite body.
func TestDoStream_RequestBodyReflectsPostTransform(t *testing.T) {
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	model, err := New(Config{APIKey: "test-key", BaseURL: server.URL}).LanguageModel("llama3.1-8b")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}

	maxTokens := 256
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		MaxTokens: &maxTokens,
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer stream.Close() //nolint:errcheck

	rb, ok := stream.(provider.StreamRequestBody)
	if !ok {
		t.Fatalf("stream (%T) does not implement provider.StreamRequestBody", stream)
	}
	body, ok := rb.RequestBody().(map[string]interface{})
	if !ok {
		t.Fatalf("RequestBody() = %#v, want a map[string]interface{}", rb.RequestBody())
	}

	// The exposed body must already reflect Cerebras's max_tokens ->
	// max_completion_tokens rename, matching what was actually sent.
	if _, present := body["max_tokens"]; present {
		t.Errorf("RequestBody() still has max_tokens = %v, want it renamed to max_completion_tokens", body["max_tokens"])
	}
	// body comes straight from the Go request builder (pre-JSON), so
	// max_completion_tokens is still an int there.
	if mct, ok := body["max_completion_tokens"].(int); !ok || mct != maxTokens {
		t.Errorf("RequestBody()[\"max_completion_tokens\"] = %#v, want %d", body["max_completion_tokens"], maxTokens)
	}

	if capturedBody == nil {
		t.Fatal("server never received a request body")
	}
	if _, present := capturedBody["max_tokens"]; present {
		t.Errorf("wire body still has max_tokens = %v", capturedBody["max_tokens"])
	}
	// capturedBody was decoded from the actual JSON sent over the wire, so
	// numbers come back as float64.
	if capturedBody["max_completion_tokens"] != float64(maxTokens) {
		t.Errorf("wire body max_completion_tokens = %v, want %v", capturedBody["max_completion_tokens"], float64(maxTokens))
	}
}
