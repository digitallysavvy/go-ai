package zai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const zaiSuccessResponseJSON = `{
	"id": "chatcmpl-123",
	"request_id": "request-123",
	"created": 1777000000,
	"model": "glm-5.3",
	"choices": [
		{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "The answer is 42.",
				"reasoning_content": "I should calculate the answer.",
				"tool_calls": [
					{"id": "call-1", "type": "function", "function": {"name": "calculator", "arguments": "{\"value\":42}"}}
				]
			},
			"finish_reason": "tool_calls"
		}
	],
	"usage": {
		"prompt_tokens": 10,
		"completion_tokens": 7,
		"prompt_tokens_details": {"cached_tokens": 3},
		"total_tokens": 17
	}
}`

func newZaiTestServer(t *testing.T, handler http.HandlerFunc) (*Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(Config{APIKey: "test-key", BaseURL: srv.URL}), srv
}

// Ported from zai-chat-language-model.test.ts "maps Z.AI provider options and
// omits unsupported standard options".
func TestDoGenerateMapsZaiProviderOptionsAndOmitsUnsupported(t *testing.T) {
	var capturedBody map[string]interface{}
	p, _ := newZaiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(zaiSuccessResponseJSON))
	})
	model := NewLanguageModel(p, "glm-5.3")

	fp := 0.2
	pp := 0.3
	seed := 42
	reasoning := types.ReasoningLow
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "Hello"},
		FrequencyPenalty: &fp,
		PresencePenalty:  &pp,
		Seed:             &seed,
		Reasoning:        &reasoning,
		ToolChoice:       types.ToolChoice{Type: types.ToolChoiceRequired},
		Tools: []types.Tool{{
			Name:        "calculator",
			Description: "Calculate a value",
			Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		}},
		ProviderOptions: map[string]interface{}{
			"zai": map[string]interface{}{
				"doSample":        false,
				"thinking":        map[string]interface{}{"type": "enabled", "clearThinking": false},
				"reasoningEffort": "max",
				"toolStream":      true,
				"requestId":       "request-123456",
				"userId":          "user-123456",
				"ignoredOption":   true,
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	if capturedBody["model"] != "glm-5.3" {
		t.Fatalf("model = %v, want glm-5.3", capturedBody["model"])
	}
	if capturedBody["do_sample"] != false {
		t.Fatalf("do_sample = %v, want false", capturedBody["do_sample"])
	}
	thinking, _ := capturedBody["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" || thinking["clear_thinking"] != false {
		t.Fatalf("thinking = %#v, want {type:enabled, clear_thinking:false}", thinking)
	}
	if capturedBody["reasoning_effort"] != "max" {
		t.Fatalf("reasoning_effort = %v, want max", capturedBody["reasoning_effort"])
	}
	if capturedBody["tool_stream"] != true {
		t.Fatalf("tool_stream = %v, want true", capturedBody["tool_stream"])
	}
	if capturedBody["request_id"] != "request-123456" {
		t.Fatalf("request_id = %v, want request-123456", capturedBody["request_id"])
	}
	if capturedBody["user_id"] != "user-123456" {
		t.Fatalf("user_id = %v, want user-123456", capturedBody["user_id"])
	}
	for _, key := range []string{"frequency_penalty", "presence_penalty", "seed", "ignoredOption", "tool_choice"} {
		if _, ok := capturedBody[key]; ok {
			t.Fatalf("body should not have property %q: %#v", key, capturedBody)
		}
	}

	wantWarnings := map[string]bool{
		"frequencyPenalty":    false,
		"presencePenalty":     false,
		"seed":                false,
		"toolChoice required": false,
	}
	for _, w := range result.Warnings {
		if _, ok := wantWarnings[w.Feature]; ok {
			wantWarnings[w.Feature] = true
		}
	}
	for feature, seen := range wantWarnings {
		if !seen {
			t.Errorf("missing warning for feature %q; got %+v", feature, result.Warnings)
		}
	}
}

// Ported from zai-chat-language-model.test.ts "implements toolChoice none by
// omitting tools".
func TestDoGenerateToolChoiceNoneOmitsTools(t *testing.T) {
	var capturedBody map[string]interface{}
	p, _ := newZaiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(zaiSuccessResponseJSON))
	})
	model := NewLanguageModel(p, "glm-5.3")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:     types.Prompt{Text: "Hello"},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceNone},
		Tools: []types.Tool{{
			Name:       "calculator",
			Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		}},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if _, ok := capturedBody["tools"]; ok {
		t.Fatalf("body should not have tools: %#v", capturedBody)
	}
	if _, ok := capturedBody["tool_choice"]; ok {
		t.Fatalf("body should not have tool_choice: %#v", capturedBody)
	}
}

// Ported from zai-chat-language-model.test.ts "validates Z.AI provider
// options".
func TestDoGenerateValidatesZaiProviderOptions(t *testing.T) {
	called := false
	p, _ := newZaiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(zaiSuccessResponseJSON))
	})
	model := NewLanguageModel(p, "glm-5.3")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
		ProviderOptions: map[string]interface{}{
			"zai": map[string]interface{}{"requestId": "short"},
		},
	})
	if err == nil {
		t.Fatal("DoGenerate() expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid zai provider options") {
		t.Fatalf("error = %v, want it to contain 'invalid zai provider options'", err)
	}
	if called {
		t.Fatal("fetch should not have been called")
	}
}

// Ported from zai-chat-language-model.test.ts "parses text, reasoning, tool
// calls, cached usage, and finish reason".
func TestDoGenerateParsesTextReasoningToolCallsUsage(t *testing.T) {
	p, _ := newZaiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(zaiSuccessResponseJSON))
	})
	model := NewLanguageModel(p, "glm-5.3")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if result.Text != "The answer is 42." {
		t.Fatalf("Text = %q", result.Text)
	}
	if len(result.Content) != 1 {
		t.Fatalf("Content = %#v, want 1 reasoning part", result.Content)
	}
	rc, ok := result.Content[0].(types.ReasoningContent)
	if !ok || rc.Text != "I should calculate the answer." {
		t.Fatalf("Content[0] = %#v", result.Content[0])
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ToolName != "calculator" || result.ToolCalls[0].ID != "call-1" {
		t.Fatalf("ToolCalls = %#v", result.ToolCalls)
	}
	if result.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("FinishReason = %v, want tool-calls", result.FinishReason)
	}
	if result.Usage.InputTokens == nil || *result.Usage.InputTokens != 10 {
		t.Fatalf("InputTokens = %v, want 10", result.Usage.InputTokens)
	}
	if result.Usage.InputDetails == nil || result.Usage.InputDetails.CacheReadTokens == nil || *result.Usage.InputDetails.CacheReadTokens != 3 {
		t.Fatalf("InputDetails = %#v, want cacheRead 3", result.Usage.InputDetails)
	}
	if result.ResponseMetadata == nil || result.ResponseMetadata.ID != "chatcmpl-123" || result.ResponseMetadata.ModelID != "glm-5.3" {
		t.Fatalf("ResponseMetadata = %#v", result.ResponseMetadata)
	}
}

// Ported from zai-chat-language-model.test.ts "maps the %s finish reason"
// (it.each: sensitive/content-filter, model_context_window_exceeded/length,
// network_error/error).
func TestDoGenerateMapsZaiSpecificFinishReasons(t *testing.T) {
	cases := []struct {
		raw  string
		want types.FinishReason
	}{
		{"sensitive", types.FinishReasonContentFilter},
		{"model_context_window_exceeded", types.FinishReasonLength},
		{"network_error", types.FinishReasonError},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			body := strings.Replace(zaiSuccessResponseJSON, `"finish_reason": "tool_calls"`, `"finish_reason": "`+tc.raw+`"`, 1)
			p, _ := newZaiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			})
			model := NewLanguageModel(p, "glm-5.3")
			result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt: types.Prompt{Text: "Hello"},
			})
			if err != nil {
				t.Fatalf("DoGenerate() error = %v", err)
			}
			if result.FinishReason != tc.want {
				t.Fatalf("FinishReason = %v, want %v", result.FinishReason, tc.want)
			}
		})
	}
}

// Ported from zai-chat-language-model.test.ts "streams reasoning, text,
// usage, raw chunks, and tool-stream options".
func TestDoStreamReasoningTextUsageToolStream(t *testing.T) {
	var capturedBody map[string]interface{}
	streamChunks := []string{
		`{"id":"chatcmpl-stream","created":1777000000,"model":"glm-5.3","choices":[{"delta":{"role":"assistant","reasoning_content":"Think."},"finish_reason":null}]}`,
		`{"id":"chatcmpl-stream","created":1777000000,"model":"glm-5.3","choices":[{"delta":{"content":"Answer."},"finish_reason":null}]}`,
		`{"id":"chatcmpl-stream","created":1777000000,"model":"glm-5.3","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`{"id":"chatcmpl-stream","created":1777000000,"model":"glm-5.3","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`,
	}
	p, _ := newZaiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range streamChunks {
			_, _ = w.Write([]byte("data: " + c + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	})
	model := NewLanguageModel(p, "glm-5.3")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "Hello"},
		IncludeRawChunks: true,
		ProviderOptions: map[string]interface{}{
			"zai": map[string]interface{}{"toolStream": true},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer stream.Close()

	var types_ []provider.ChunkType
	var finishChunk *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		types_ = append(types_, chunk.Type)
		if chunk.Type == provider.ChunkTypeFinish {
			finishChunk = chunk
		}
	}

	if capturedBody["stream"] != true || capturedBody["tool_stream"] != true {
		t.Fatalf("body = %#v, want stream:true tool_stream:true", capturedBody)
	}
	if _, ok := capturedBody["stream_options"]; ok {
		t.Fatalf("body should not have stream_options: %#v", capturedBody)
	}

	// TS defers the "finish" part's emission to the stream's flush()
	// callback, which only runs once the whole SSE stream (including a
	// trailing choices-less usage event) has been consumed, and merges that
	// event's usage into "finish". The shared
	// providerutils/streaming.OpenAICompatStream base this Go provider embeds
	// mirrors that: it holds the finish chunk back when finish_reason arrives
	// and only enqueues it once the stream ends ([DONE]/EOF), by which point
	// the trailing usage-only event has already been merged in. So the raw
	// chunk for that trailing event is emitted (IncludeRawChunks is
	// unconditional) before "finish", not after.
	wantSequence := []provider.ChunkType{
		provider.ChunkTypeStreamStart,
		provider.ChunkTypeRaw,
		provider.ChunkTypeResponseMetadata,
		provider.ChunkTypeReasoningStart,
		provider.ChunkTypeReasoning,
		provider.ChunkTypeRaw,
		provider.ChunkTypeReasoningEnd,
		provider.ChunkTypeText,
		provider.ChunkTypeRaw,
		provider.ChunkTypeRaw,
		provider.ChunkTypeFinish,
	}
	if len(types_) != len(wantSequence) {
		t.Fatalf("chunk sequence = %v, want length %d", types_, len(wantSequence))
	}
	for i, want := range wantSequence {
		if types_[i] != want {
			t.Fatalf("chunk[%d] = %v, want %v (full: %v)", i, types_[i], want, types_)
		}
	}
	if finishChunk == nil || finishChunk.FinishReason != types.FinishReasonStop {
		t.Fatalf("finish chunk = %#v, want finishReason stop", finishChunk)
	}
	if finishChunk.Usage == nil || finishChunk.Usage.InputTokens == nil || *finishChunk.Usage.InputTokens != 4 {
		t.Fatalf("finish usage = %#v, want inputTokens 4 (merged from trailing usage-only chunk)", finishChunk.Usage)
	}
	if finishChunk.Usage.OutputTokens == nil || *finishChunk.Usage.OutputTokens != 3 {
		t.Fatalf("finish usage = %#v, want outputTokens 3", finishChunk.Usage)
	}
	if finishChunk.Usage.TotalTokens == nil || *finishChunk.Usage.TotalTokens != 7 {
		t.Fatalf("finish usage = %#v, want totalTokens 7", finishChunk.Usage)
	}
}

// Ported from zai-chat-language-model.test.ts "streams incremental tool-call
// arguments".
func TestDoStreamIncrementalToolCallArguments(t *testing.T) {
	streamChunks := []string{
		`{"id":"chatcmpl-tool","created":1777000000,"model":"glm-5.3","choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-weather","function":{"name":"weather","arguments":"{\"city\""}}]},"finish_reason":null}]}`,
		`{"id":"chatcmpl-tool","created":1777000000,"model":"glm-5.3","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"Paris\"}"}}]},"finish_reason":null}]}`,
		`{"id":"chatcmpl-tool","created":1777000000,"model":"glm-5.3","choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":4,"total_tokens":9}}`,
	}
	p, _ := newZaiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range streamChunks {
			_, _ = w.Write([]byte("data: " + c + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	})
	model := NewLanguageModel(p, "glm-5.3")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
		Tools: []types.Tool{{
			Name:       "weather",
			Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		}},
		ProviderOptions: map[string]interface{}{
			"zai": map[string]interface{}{"toolStream": true},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer stream.Close()

	var toolCallChunk *provider.StreamChunk
	var finishChunk *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeToolCall {
			toolCallChunk = chunk
		}
		if chunk.Type == provider.ChunkTypeFinish {
			finishChunk = chunk
		}
	}
	if toolCallChunk == nil || toolCallChunk.ToolCall == nil {
		t.Fatal("expected a tool-call chunk")
	}
	if toolCallChunk.ToolCall.ToolName != "weather" || toolCallChunk.ToolCall.ID != "call-weather" {
		t.Fatalf("tool call = %#v", toolCallChunk.ToolCall)
	}
	if toolCallChunk.ToolCall.Arguments["city"] != "Paris" {
		t.Fatalf("tool call arguments = %#v, want city=Paris", toolCallChunk.ToolCall.Arguments)
	}
	if finishChunk == nil || finishChunk.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("finish chunk = %#v, want tool-calls", finishChunk)
	}
	if finishChunk.Usage == nil || finishChunk.Usage.InputTokens == nil || *finishChunk.Usage.InputTokens != 5 {
		t.Fatalf("finish usage = %#v, want inputTokens 5", finishChunk.Usage)
	}
	if finishChunk.Usage.OutputTokens == nil || *finishChunk.Usage.OutputTokens != 4 {
		t.Fatalf("finish usage = %#v, want outputTokens 4", finishChunk.Usage)
	}
}

// Ported from zai-chat-language-model.test.ts "parses the documented Z.AI
// error envelope".
func TestDoGenerateParsesDocumentedErrorEnvelope(t *testing.T) {
	p, _ := newZaiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":1001,"message":"Invalid request."}`))
	})
	model := NewLanguageModel(p, "glm-5.3")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
	})
	if err == nil {
		t.Fatal("DoGenerate() expected error, got nil")
	}
	provErr, ok := err.(*providererrors.ProviderError)
	if !ok {
		t.Fatalf("expected *providererrors.ProviderError, got %T: %v", err, err)
	}
	if provErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want 400", provErr.StatusCode)
	}
	if provErr.Message != "Invalid request." {
		t.Fatalf("Message = %q, want %q", provErr.Message, "Invalid request.")
	}
}
