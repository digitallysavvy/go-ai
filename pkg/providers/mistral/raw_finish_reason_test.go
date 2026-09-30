package mistral

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestMistralDoGenerateRawFinishReason verifies DoGenerate surfaces the raw
// choice.finish_reason string, mirroring TS mistral-chat-language-model.ts's
// `finishReason: { unified: mapMistralFinishReason(...), raw:
// choice.finish_reason ?? undefined }`.
func TestMistralDoGenerateRawFinishReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","choices":[{"index":0,"finish_reason":"model_length","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "mistral-large-latest")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if result.FinishReason != types.FinishReasonLength {
		t.Fatalf("FinishReason = %v, want length", result.FinishReason)
	}
	if result.RawFinishReason != "model_length" {
		t.Fatalf("RawFinishReason = %q, want %q", result.RawFinishReason, "model_length")
	}
}

// TestMistralDoStreamRawFinishReason verifies the stream's terminal finish
// chunk carries the raw finish_reason string.
func TestMistralDoStreamRawFinishReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"model_length\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "mistral-large-latest")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
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
	if finish.RawFinishReason != "model_length" {
		t.Fatalf("RawFinishReason = %q, want %q", finish.RawFinishReason, "model_length")
	}
}
