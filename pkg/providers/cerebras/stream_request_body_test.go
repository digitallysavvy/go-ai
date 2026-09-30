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
