package huggingface

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func sseHandler(chunks []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// newSSECaptureServer starts a test server that decodes the request body
// JSON into *gotBody before serving the given SSE chunks.
func newSSECaptureServer(t *testing.T, gotBody *map[string]interface{}, chunks []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotBody = jsonBody(t, r)
		sseHandler(chunks)(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func collectChunks(t *testing.T, stream provider.TextStream) []*provider.StreamChunk {
	t.Helper()
	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next(): %v", err)
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

func findChunk(chunks []*provider.StreamChunk, t provider.ChunkType) *provider.StreamChunk {
	for _, c := range chunks {
		if c.Type == t {
			return c
		}
	}
	return nil
}

// Ported from "doStream > should stream text deltas".
func TestDoStreamTextDeltas(t *testing.T) {
	model, _ := newTestModel(t, sseHandler([]string{
		"data:{\"type\":\"response.created\",\"response\":{\"id\":\"resp_test\",\"object\":\"response\",\"created_at\":1741269019,\"status\":\"in_progress\",\"model\":\"deepseek-ai/DeepSeek-V3-0324\"}}\n\n",
		"data:{\"type\":\"response.in_progress\",\"response\":{\"id\":\"resp_test\",\"object\":\"response\",\"created_at\":1741269019,\"status\":\"in_progress\"}}\n\n",
		"data:{\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"in_progress\",\"content\":[]},\"sequence_number\":1}\n\n",
		"data:{\"type\":\"response.output_text.delta\",\"item_id\":\"msg_test\",\"output_index\":0,\"content_index\":0,\"delta\":\"Hello,\",\"sequence_number\":2}\n\n",
		"data:{\"type\":\"response.output_text.delta\",\"item_id\":\"msg_test\",\"output_index\":0,\"content_index\":0,\"delta\":\" World!\",\"sequence_number\":3}\n\n",
		"data:{\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"Hello, World!\"}]},\"sequence_number\":4}\n\n",
		"data:{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"model\":\"deepseek-ai/DeepSeek-V3-0324\",\"object\":\"response\",\"created_at\":1741269112,\"status\":\"completed\",\"incomplete_details\":null,\"usage\":{\"input_tokens\":12,\"output_tokens\":25,\"total_tokens\":37},\"output\":[]},\"sequence_number\":5}\n\n",
	}), "deepseek-ai/DeepSeek-V3-0324")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer func() { _ = stream.Close() }()
	chunks := collectChunks(t, stream)

	wantTypes := []provider.ChunkType{
		provider.ChunkTypeStreamStart,
		provider.ChunkTypeResponseMetadata,
		provider.ChunkTypeTextStart,
		provider.ChunkTypeText,
		provider.ChunkTypeText,
		provider.ChunkTypeTextEnd,
		provider.ChunkTypeFinish,
	}
	if len(chunks) != len(wantTypes) {
		t.Fatalf("chunks = %d, want %d: %#v", len(chunks), len(wantTypes), chunks)
	}
	for i, want := range wantTypes {
		if chunks[i].Type != want {
			t.Fatalf("chunks[%d].Type = %q, want %q", i, chunks[i].Type, want)
		}
	}
	if chunks[2].ID != "msg_test" {
		t.Fatalf("text-start ID = %q", chunks[2].ID)
	}
	if chunks[3].Text != "Hello," || chunks[4].Text != " World!" {
		t.Fatalf("text deltas = %q, %q", chunks[3].Text, chunks[4].Text)
	}
	finish := chunks[6]
	if finish.FinishReason != types.FinishReasonStop {
		t.Fatalf("FinishReason = %q", finish.FinishReason)
	}
	if finish.Usage == nil || *finish.Usage.InputTokens != 12 || *finish.Usage.OutputTokens != 25 {
		t.Fatalf("Usage = %#v", finish.Usage)
	}
	var meta map[string]map[string]string
	_ = json.Unmarshal(finish.ProviderMetadata, &meta)
	if meta["huggingface"]["responseId"] != "resp_test" {
		t.Fatalf("responseId = %#v", meta)
	}
}

// Ported from "doStream > should handle streaming without usage".
func TestDoStreamWithoutUsage(t *testing.T) {
	model, _ := newTestModel(t, sseHandler([]string{
		"data:{\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"in_progress\"},\"sequence_number\":1}\n\n",
		"data:{\"type\":\"response.output_text.delta\",\"item_id\":\"msg_test\",\"output_index\":0,\"content_index\":0,\"delta\":\"Hi!\",\"sequence_number\":2}\n\n",
		"data:{\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\"},\"sequence_number\":3}\n\n",
		"data:{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"status\":\"completed\",\"incomplete_details\":null,\"usage\":null},\"sequence_number\":4}\n\n",
	}), "m")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer func() { _ = stream.Close() }()
	chunks := collectChunks(t, stream)
	finish := findChunk(chunks, provider.ChunkTypeFinish)
	if finish == nil {
		t.Fatal("no finish chunk")
	}
	if finish.Usage == nil || finish.Usage.InputTokens != nil || finish.Usage.OutputTokens != nil {
		t.Fatalf("Usage = %#v, want all nil", finish.Usage)
	}
}

// Ported from "doStream > should handle non-message item types".
func TestDoStreamNonMessageItemTypes(t *testing.T) {
	model, _ := newTestModel(t, sseHandler([]string{
		"data:{\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"mcp_test\",\"type\":\"mcp_list_tools\",\"server_label\":\"test\"},\"sequence_number\":1}\n\n",
		"data:{\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"mcp_test\",\"type\":\"mcp_list_tools\",\"server_label\":\"test\"},\"sequence_number\":2}\n\n",
		"data:{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"status\":\"completed\",\"incomplete_details\":null},\"sequence_number\":3}\n\n",
	}), "m")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer func() { _ = stream.Close() }()
	chunks := collectChunks(t, stream)
	if len(chunks) != 2 || chunks[0].Type != provider.ChunkTypeStreamStart || chunks[1].Type != provider.ChunkTypeFinish {
		types := make([]provider.ChunkType, len(chunks))
		for i, c := range chunks {
			types[i] = c.Type
		}
		t.Fatalf("chunk types = %v, want [stream-start finish]", types)
	}
}

// Ported from "doStream > should handle streaming errors" (malformed JSON).
func TestDoStreamMalformedJSON(t *testing.T) {
	model, _ := newTestModel(t, sseHandler([]string{
		"data:{\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\"},\"sequence_number\":1}\n\n",
		"data:invalid json}\n\n",
	}), "m")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer func() { _ = stream.Close() }()
	chunks := collectChunks(t, stream)
	errChunk := findChunk(chunks, provider.ChunkTypeError)
	if errChunk == nil {
		t.Fatal("expected an error chunk")
	}
	finish := findChunk(chunks, provider.ChunkTypeFinish)
	if finish == nil || finish.FinishReason != types.FinishReasonError {
		t.Fatalf("finish = %#v, want finishReason error", finish)
	}
}

// Ported from "doStream > preserves $expectedType stream errors" (it.each).
func TestDoStreamPreservesStreamErrors(t *testing.T) {
	cases := []struct {
		name        string
		event       string
		wantType    string
		wantMessage string
		wantCode    interface{}
	}{
		{
			name:        "response.failed",
			event:       `{"type":"response.failed","response":{"error":{"code":"429","message":"Rate limit reached"}},"sequence_number":1}`,
			wantType:    "response.failed",
			wantMessage: "Rate limit reached",
			wantCode:    "429",
		},
		{
			name:        "error",
			event:       `{"type":"error","code":"503","message":"Service unavailable","param":null,"sequence_number":1}`,
			wantType:    "error",
			wantMessage: "Service unavailable",
			wantCode:    "503",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model, _ := newTestModel(t, sseHandler([]string{"data:" + tc.event + "\n\n"}), "m")

			stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
			if err != nil {
				t.Fatalf("DoStream: %v", err)
			}
			defer func() { _ = stream.Close() }()
			chunks := collectChunks(t, stream)

			errChunk := findChunk(chunks, provider.ChunkTypeError)
			if errChunk == nil {
				t.Fatal("expected an error chunk")
			}
			streamErr, ok := errChunk.Err.(*providererrors.StreamProviderError)
			if !ok {
				t.Fatalf("Err type = %T, want *providererrors.StreamProviderError", errChunk.Err)
			}
			if streamErr.Message != tc.wantMessage {
				t.Fatalf("Message = %q, want %q", streamErr.Message, tc.wantMessage)
			}
			if streamErr.Type != tc.wantType {
				t.Fatalf("Type = %q, want %q", streamErr.Type, tc.wantType)
			}
			if streamErr.Code != tc.wantCode {
				t.Fatalf("Code = %#v, want %#v", streamErr.Code, tc.wantCode)
			}

			last := chunks[len(chunks)-1]
			if last.Type != provider.ChunkTypeFinish || last.FinishReason != types.FinishReasonError {
				t.Fatalf("last chunk = %#v, want finish/error", last)
			}
		})
	}
}

// Ported from "doStream > should send correct streaming request".
func TestDoStreamRequestBody(t *testing.T) {
	var gotBody map[string]interface{}
	srv := newSSECaptureServer(t, &gotBody, []string{
		"data:{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"status\":\"completed\"},\"sequence_number\":1}\n\n",
	})
	p := New(Config{APIKey: "APIKEY", BaseURL: srv.URL})
	model := NewLanguageModel(p, "deepseek-ai/DeepSeek-V3-0324")

	temp := 0.7
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: testPrompt, Temperature: &temp})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer func() { _ = stream.Close() }()
	_ = collectChunks(t, stream)

	want := map[string]interface{}{
		"model":       "deepseek-ai/DeepSeek-V3-0324",
		"temperature": 0.7,
		"stream":      true,
		"input": []interface{}{
			map[string]interface{}{"role": "user", "content": []interface{}{
				map[string]interface{}{"type": "input_text", "text": "Hello"},
			}},
		},
	}
	assertJSONEqual(t, gotBody, want)
}

// Ported from "tool calls > should stream tool calls".
func TestDoStreamToolCalls(t *testing.T) {
	model, _ := newTestModel(t, sseHandler([]string{
		"data:{\"type\":\"response.created\",\"response\":{\"id\":\"resp_tool_stream\",\"object\":\"response\",\"created_at\":1741269019,\"status\":\"in_progress\",\"model\":\"deepseek-ai/DeepSeek-V3-0324\"}}\n\n",
		"data:{\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"fc_stream\",\"type\":\"function_call\",\"call_id\":\"call_456\",\"name\":\"calculator\",\"arguments\":\"\"},\"sequence_number\":1}\n\n",
		"data:{\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"fc_stream\",\"type\":\"function_call\",\"call_id\":\"call_456\",\"name\":\"calculator\",\"arguments\":\"{\\\"operation\\\": \\\"add\\\", \\\"a\\\": 5, \\\"b\\\": 3}\",\"output\":\"8\"},\"sequence_number\":4}\n\n",
		"data:{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_tool_stream\",\"status\":\"completed\",\"usage\":{\"input_tokens\":20,\"output_tokens\":15,\"total_tokens\":35}},\"sequence_number\":5}\n\n",
	}), "deepseek-ai/DeepSeek-V3-0324")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer func() { _ = stream.Close() }()
	chunks := collectChunks(t, stream)

	wantTypes := []provider.ChunkType{
		provider.ChunkTypeStreamStart,
		provider.ChunkTypeResponseMetadata,
		provider.ChunkTypeToolInputStart,
		provider.ChunkTypeToolInputEnd,
		provider.ChunkTypeToolCall,
		provider.ChunkTypeToolResult,
		provider.ChunkTypeFinish,
	}
	if len(chunks) != len(wantTypes) {
		t.Fatalf("chunks = %d, want %d: %#v", len(chunks), len(wantTypes), chunks)
	}
	for i, want := range wantTypes {
		if chunks[i].Type != want {
			t.Fatalf("chunks[%d].Type = %q, want %q", i, chunks[i].Type, want)
		}
	}
	if chunks[2].ToolCall.ID != "call_456" || chunks[2].ToolCall.ToolName != "calculator" {
		t.Fatalf("tool-input-start = %#v", chunks[2].ToolCall)
	}
	toolCall := chunks[4].ToolCall
	if toolCall.ID != "call_456" || toolCall.ToolName != "calculator" || toolCall.Arguments["operation"] != "add" {
		t.Fatalf("tool-call = %#v", toolCall)
	}
	toolResult := chunks[5].ToolResult
	if toolResult.ToolCallID != "call_456" || toolResult.Result != "8" {
		t.Fatalf("tool-result = %#v", toolResult)
	}
}

// Ported from "reasoning > should stream reasoning content".
func TestDoStreamReasoningContent(t *testing.T) {
	model, _ := newTestModel(t, sseHandler([]string{
		"data:{\"type\":\"response.created\",\"response\":{\"id\":\"resp_reasoning_stream\",\"object\":\"response\",\"created_at\":1741269019,\"status\":\"in_progress\",\"model\":\"deepseek-ai/DeepSeek-R1\"}}\n\n",
		"data:{\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"reasoning_stream\",\"type\":\"reasoning\"},\"sequence_number\":1}\n\n",
		"data:{\"type\":\"response.reasoning_text.delta\",\"item_id\":\"reasoning_stream\",\"output_index\":0,\"content_index\":0,\"delta\":\"Thinking about\",\"sequence_number\":2}\n\n",
		"data:{\"type\":\"response.reasoning_text.delta\",\"item_id\":\"reasoning_stream\",\"output_index\":0,\"content_index\":0,\"delta\":\" the problem...\",\"sequence_number\":3}\n\n",
		"data:{\"type\":\"response.reasoning_text.done\",\"item_id\":\"reasoning_stream\",\"output_index\":0,\"content_index\":0,\"text\":\"Thinking about the problem...\",\"sequence_number\":4}\n\n",
		"data:{\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"reasoning_stream\",\"type\":\"reasoning\",\"content\":[{\"type\":\"reasoning_text\",\"text\":\"Thinking about the problem...\"}]},\"sequence_number\":5}\n\n",
		"data:{\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":{\"id\":\"msg_stream\",\"type\":\"message\",\"role\":\"assistant\"},\"sequence_number\":6}\n\n",
		"data:{\"type\":\"response.output_text.delta\",\"item_id\":\"msg_stream\",\"output_index\":1,\"content_index\":0,\"delta\":\"The solution is\",\"sequence_number\":7}\n\n",
		"data:{\"type\":\"response.output_text.delta\",\"item_id\":\"msg_stream\",\"output_index\":1,\"content_index\":0,\"delta\":\" simple.\",\"sequence_number\":8}\n\n",
		"data:{\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"id\":\"msg_stream\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"The solution is simple.\"}]},\"sequence_number\":9}\n\n",
		"data:{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_reasoning_stream\",\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":20,\"total_tokens\":30}},\"sequence_number\":10}\n\n",
	}), "deepseek-ai/DeepSeek-R1")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer func() { _ = stream.Close() }()
	chunks := collectChunks(t, stream)

	wantTypes := []provider.ChunkType{
		provider.ChunkTypeStreamStart,
		provider.ChunkTypeResponseMetadata,
		provider.ChunkTypeReasoningStart,
		provider.ChunkTypeReasoning,
		provider.ChunkTypeReasoning,
		provider.ChunkTypeReasoningEnd,
		provider.ChunkTypeTextStart,
		provider.ChunkTypeText,
		provider.ChunkTypeText,
		provider.ChunkTypeTextEnd,
		provider.ChunkTypeFinish,
	}
	if len(chunks) != len(wantTypes) {
		t.Fatalf("chunks = %d, want %d: %#v", len(chunks), len(wantTypes), chunks)
	}
	for i, want := range wantTypes {
		if chunks[i].Type != want {
			t.Fatalf("chunks[%d].Type = %q, want %q", i, chunks[i].Type, want)
		}
	}
	if chunks[2].ID != "reasoning_stream" {
		t.Fatalf("reasoning-start ID = %q", chunks[2].ID)
	}
	if chunks[3].Reasoning != "Thinking about" || chunks[4].Reasoning != " the problem..." {
		t.Fatalf("reasoning deltas = %q, %q", chunks[3].Reasoning, chunks[4].Reasoning)
	}
}
