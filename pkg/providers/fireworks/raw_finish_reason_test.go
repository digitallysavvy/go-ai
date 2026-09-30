package fireworks

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestFireworksDoGenerateRawFinishReason verifies DoGenerate surfaces the raw
// provider finish_reason string on types.GenerateResult, mirroring TS
// openai-compatible-chat-language-model.ts's `finishReason: { unified, raw:
// choice.finish_reason ?? undefined }` (Fireworks embeds the shared
// OpenAICompatStream / openai-compatible convertResponse shape).
func TestFireworksDoGenerateRawFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	model, err := New(Config{APIKey: "test-key", BaseURL: server.URL}).LanguageModel("accounts/fireworks/models/test")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if result.FinishReason != types.FinishReasonStop {
		t.Fatalf("FinishReason = %v, want stop", result.FinishReason)
	}
	if result.RawFinishReason != "stop" {
		t.Fatalf("RawFinishReason = %q, want %q", result.RawFinishReason, "stop")
	}
}

// TestFireworksDoStreamRawFinishReason verifies DoStream's terminal finish
// chunk carries the raw finish_reason string via the shared
// OpenAICompatStream (see openai_compat_stream.go's endStream/
// flushToolCallsAndFinish).
func TestFireworksDoStreamRawFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	model, err := New(Config{APIKey: "test-key", BaseURL: server.URL}).LanguageModel("accounts/fireworks/models/test")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	if err != nil {
		t.Fatalf("DoStream error = %v", err)
	}
	defer stream.Close() //nolint:errcheck

	var finish *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next error = %v", err)
		}
		if chunk.Type == provider.ChunkTypeFinish {
			finish = chunk
		}
	}
	if finish == nil {
		t.Fatal("no finish chunk observed")
	}
	if finish.FinishReason != types.FinishReasonLength {
		t.Fatalf("FinishReason = %v, want length", finish.FinishReason)
	}
	if finish.RawFinishReason != "length" {
		t.Fatalf("RawFinishReason = %q, want %q", finish.RawFinishReason, "length")
	}
}
