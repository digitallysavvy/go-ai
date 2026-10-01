package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestDoStream_ImplementsStreamRequestBody verifies that the TextStream
// returned by DoStream exposes the raw request body it sent via the
// optional provider.StreamRequestBody capability (hand-off: "stream request
// body field"), through the providerutils.WithResponseMetadata wrapper.
func TestDoStream_ImplementsStreamRequestBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}` + "\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModel(ClaudeSonnet4_5)
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		MaxTokens: intPtr(100),
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
	if body["model"] != string(ClaudeSonnet4_5) {
		t.Errorf("RequestBody()[\"model\"] = %v, want %q", body["model"], ClaudeSonnet4_5)
	}
	if body["stream"] != true {
		t.Errorf("RequestBody()[\"stream\"] = %v, want true", body["stream"])
	}
}
