package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestAnthropicDoGenerateRawFinishReason verifies DoGenerate surfaces the raw
// stop_reason string on types.GenerateResult, mirroring TS
// anthropic-language-model.ts's `finishReason: { unified:
// mapAnthropicStopReason(...), raw: response.stop_reason ?? undefined }`.
func TestAnthropicDoGenerateRawFinishReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_1",
			"type": "message",
			"role": "assistant",
			"model": "claude-3-haiku-20240307",
			"content": [{"type": "text", "text": "hi"}],
			"stop_reason": "max_tokens",
			"usage": {"input_tokens": 4, "output_tokens": 30}
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModel(ClaudeSonnet4_5)
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if result.FinishReason != types.FinishReasonLength {
		t.Fatalf("FinishReason = %v, want length", result.FinishReason)
	}
	if result.RawFinishReason != "max_tokens" {
		t.Fatalf("RawFinishReason = %q, want %q", result.RawFinishReason, "max_tokens")
	}
}

// TestAnthropicDoStreamRawFinishReason verifies the stream's terminal finish
// chunk carries the raw stop_reason from the message_delta event, mirroring
// TS's `finishReason = { unified: mapAnthropicStopReason(...), raw:
// value.delta.stop_reason ?? undefined }`.
func TestAnthropicDoStreamRawFinishReason(t *testing.T) {
	body := io.NopCloser(strings.NewReader(
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	))

	stream := newAnthropicStream(body, false)
	var finish *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}
		if chunk.Type == provider.ChunkTypeFinish {
			finish = chunk
		}
	}
	if finish == nil {
		t.Fatal("no finish chunk observed")
	}
	if finish.FinishReason != types.FinishReasonStop {
		t.Fatalf("FinishReason = %v, want stop", finish.FinishReason)
	}
	if finish.RawFinishReason != "end_turn" {
		t.Fatalf("RawFinishReason = %q, want %q", finish.RawFinishReason, "end_turn")
	}
}
