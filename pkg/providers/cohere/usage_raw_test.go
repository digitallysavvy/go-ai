package cohere

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestCohereDoGeneratePreservesFullRawUsage ports TS
// "convertCohereUsage" behavior (cohere/src/convert-cohere-usage.ts):
// Usage.Raw must be the complete usage object -- billed_units and
// cached_tokens included -- not just the tokens the SDK interprets (0599400).
func TestCohereDoGeneratePreservesFullRawUsage(t *testing.T) {
	const body = `{"generation_id":"test","message":{"role":"assistant","content":[{"type":"text","text":"hi"}],"tool_calls":null},"finish_reason":"COMPLETE","usage":{"billed_units":{"input_tokens":4,"output_tokens":2},"tokens":{"input_tokens":5,"output_tokens":3},"cached_tokens":1}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "command-r-plus")

	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if *result.Usage.InputTokens != 5 || *result.Usage.OutputTokens != 3 {
		t.Fatalf("unexpected token counts: %#v", result.Usage)
	}

	raw := result.Usage.Raw
	if raw == nil {
		t.Fatalf("expected Raw to be non-nil")
	}
	if _, ok := raw["billed_units"]; !ok {
		t.Fatalf("expected Raw to include billed_units, got %#v", raw)
	}
	if _, ok := raw["cached_tokens"]; !ok {
		t.Fatalf("expected Raw to include cached_tokens, got %#v", raw)
	}
	if _, ok := raw["tokens"]; !ok {
		t.Fatalf("expected Raw to include tokens, got %#v", raw)
	}
}

// TestCohereDoStreamPreservesFullRawUsage is the streaming counterpart of
// TestCohereDoGeneratePreservesFullRawUsage: the message-end usage object
// must also carry the full raw object through to the finish chunk (0599400).
func TestCohereDoStreamPreservesFullRawUsage(t *testing.T) {
	const sse = "event: message-start\n" +
		"data: {\"type\":\"message-start\",\"delta\":{\"message\":{\"role\":\"assistant\"}}}\n\n" +
		"event: content-start\n" +
		"data: {\"type\":\"content-start\",\"index\":0,\"delta\":{\"message\":{\"content\":{\"type\":\"text\",\"text\":\"\"}}}}\n\n" +
		"event: content-delta\n" +
		"data: {\"type\":\"content-delta\",\"index\":0,\"delta\":{\"message\":{\"content\":{\"text\":\"hi\"}}}}\n\n" +
		"event: message-end\n" +
		"data: {\"type\":\"message-end\",\"delta\":{\"finish_reason\":\"COMPLETE\",\"usage\":{\"billed_units\":{\"input_tokens\":4,\"output_tokens\":2},\"tokens\":{\"input_tokens\":5,\"output_tokens\":3},\"cached_tokens\":1}}}\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "command-r-plus")

	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var finish *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk == nil {
			break
		}
		if chunk.Type == provider.ChunkTypeFinish {
			c := *chunk
			finish = &c
			break
		}
	}
	if finish == nil || finish.Usage == nil {
		t.Fatalf("expected a finish chunk with usage, got %#v", finish)
	}
	raw := finish.Usage.Raw
	if raw == nil {
		t.Fatalf("expected Raw to be non-nil")
	}
	if _, ok := raw["billed_units"]; !ok {
		t.Fatalf("expected Raw to include billed_units, got %#v", raw)
	}
	if _, ok := raw["cached_tokens"]; !ok {
		t.Fatalf("expected Raw to include cached_tokens, got %#v", raw)
	}
}
