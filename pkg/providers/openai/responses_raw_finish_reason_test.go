package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestResponsesLanguageModel_DoGenerate_RawFinishReason verifies DoGenerate
// surfaces the raw incomplete_details.reason string, mirroring TS
// openai-responses-language-model.ts's `finishReason: { unified:
// mapOpenAIResponseFinishReason(...), raw: response.incomplete_details?.reason
// ?? undefined }`.
func TestResponsesLanguageModel_DoGenerate_RawFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
			"id": "resp_1",
			"model": "gpt-4o",
			"output": [],
			"incomplete_details": {"reason": "max_output_tokens"},
			"usage": {"input_tokens": 5, "output_tokens": 10}
		}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if result.FinishReason != types.FinishReasonLength {
		t.Fatalf("FinishReason = %v, want length", result.FinishReason)
	}
	if result.RawFinishReason != "max_output_tokens" {
		t.Fatalf("RawFinishReason = %q, want %q", result.RawFinishReason, "max_output_tokens")
	}
}

// TestResponsesLanguageModel_DoStream_RawFinishReason verifies the terminal
// finish chunk from a response.incomplete SSE event carries the raw
// incomplete_details.reason string.
func TestResponsesLanguageModel_DoStream_RawFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			`{"type":"response.created","response":{"id":"resp_stream","model":"gpt-4o"}}`,
			`{"type":"response.incomplete","response":{"id":"resp_stream","usage":{"input_tokens":5,"output_tokens":3},"incomplete_details":{"reason":"max_output_tokens"}}}`,
		}
		for _, e := range events {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err != nil {
		t.Fatalf("DoStream failed: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	var finishChunk *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeFinish {
			finishChunk = chunk
		}
	}
	if finishChunk == nil {
		t.Fatal("expected a finish chunk")
	}
	if finishChunk.FinishReason != types.FinishReasonLength {
		t.Fatalf("FinishReason = %v, want length", finishChunk.FinishReason)
	}
	if finishChunk.RawFinishReason != "max_output_tokens" {
		t.Fatalf("RawFinishReason = %q, want %q", finishChunk.RawFinishReason, "max_output_tokens")
	}
}
