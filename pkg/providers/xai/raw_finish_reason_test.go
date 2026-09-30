package xai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestXAIResponsesDoGenerateRawFinishReason verifies DoGenerate surfaces the
// raw response.status string, mirroring TS xai-responses-language-model.ts's
// `finishReason: { unified: hasFunctionCall ? 'tool-calls' :
// mapXaiResponsesFinishReason(response.status), raw: response.status ??
// undefined }`.
func TestXAIResponsesDoGenerateRawFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"id":     "resp_1",
			"status": "incomplete",
			"incomplete_details": map[string]interface{}{
				"reason": "max_output_tokens",
			},
			"output": []interface{}{},
			"usage":  map[string]interface{}{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "grok-3")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if result.FinishReason != types.FinishReasonLength {
		t.Fatalf("FinishReason = %v, want length", result.FinishReason)
	}
	if result.RawFinishReason != "incomplete" {
		t.Fatalf("RawFinishReason = %q, want %q", result.RawFinishReason, "incomplete")
	}
}

// TestXAIResponsesDoStreamCompletedRawFinishReason verifies the streaming
// response.completed branch's raw finish reason is response.status,
// mirroring TS's `raw: response.status` on the
// response.done/response.completed/response.incomplete event handler.
func TestXAIResponsesDoStreamCompletedRawFinishReason(t *testing.T) {
	stream := newXAIResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}

data: [DONE]

`)))
	defer stream.Close() //nolint:errcheck

	var finish *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error: %v", err)
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
	if finish.RawFinishReason != "completed" {
		t.Fatalf("RawFinishReason = %q, want %q", finish.RawFinishReason, "completed")
	}
}

// TestXAIResponsesDoStreamHasFunctionCallForcesToolCallsUnified is the
// implementer-flagged gap fix: TS xai-responses-language-model.ts's
// response.completed/response.done branch computes the *unified* finish
// reason as `hasFunctionCall ? 'tool-calls' :
// mapXaiResponsesFinishReason(response.status)` — hasFunctionCall (set on
// response.output_item.done for a function_call item) overrides whatever
// response.status says. Before this fix, the Go stream ignored
// hasFunctionCall entirely and derived the unified reason from
// response.status alone, so a function_call followed by status:"completed"
// (which normally maps to "stop") incorrectly produced FinishReasonStop
// instead of FinishReasonToolCalls. The raw string must still be the
// verbatim response.status ("completed"), unaffected by hasFunctionCall —
// TS's `raw: response.status` does not consult hasFunctionCall.
func TestXAIResponsesDoStreamHasFunctionCallForcesToolCallsUnified(t *testing.T) {
	sse := `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"get_weather"}}

data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"location\":\"NYC\"}"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"get_weather","arguments":"{\"location\":\"NYC\"}"}}

data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}

data: [DONE]

`
	stream := newXAIResponsesStream(io.NopCloser(strings.NewReader(sse)))
	defer stream.Close() //nolint:errcheck

	var toolCall *provider.StreamChunk
	var finish *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
		switch chunk.Type {
		case provider.ChunkTypeToolCall:
			toolCall = chunk
		case provider.ChunkTypeFinish:
			finish = chunk
		}
	}

	if toolCall == nil {
		t.Fatal("no tool-call chunk observed")
	}
	if toolCall.ToolCall == nil || toolCall.ToolCall.ID != "call_1" || toolCall.ToolCall.ToolName != "get_weather" {
		t.Fatalf("tool call = %#v, want call_1/get_weather", toolCall.ToolCall)
	}

	if finish == nil {
		t.Fatal("no finish chunk observed")
	}
	// response.status is "completed" (normally maps to stop), but
	// hasFunctionCall must force the unified reason to tool-calls.
	if finish.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("FinishReason = %v, want tool-calls (hasFunctionCall must override status-derived reason)", finish.FinishReason)
	}
	// raw is unaffected by hasFunctionCall: it always echoes response.status verbatim.
	if finish.RawFinishReason != "completed" {
		t.Fatalf("RawFinishReason = %q, want %q (raw is always response.status, regardless of hasFunctionCall)", finish.RawFinishReason, "completed")
	}
}

// TestXAIResponsesDoStreamHasFunctionCallWithResponseDoneAlias verifies the
// response.done alias (Gap 4) also honors hasFunctionCall the same way
// response.completed does.
func TestXAIResponsesDoStreamHasFunctionCallWithResponseDoneAlias(t *testing.T) {
	sse := `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"get_weather"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"get_weather"}}

data: {"type":"response.done","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}

data: [DONE]

`
	stream := newXAIResponsesStream(io.NopCloser(strings.NewReader(sse)))
	defer stream.Close() //nolint:errcheck

	var finish *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
		if chunk.Type == provider.ChunkTypeFinish {
			finish = chunk
		}
	}
	if finish == nil {
		t.Fatal("no finish chunk observed")
	}
	if finish.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("FinishReason = %v, want tool-calls", finish.FinishReason)
	}
	if finish.RawFinishReason != "completed" {
		t.Fatalf("RawFinishReason = %q, want %q", finish.RawFinishReason, "completed")
	}
}
