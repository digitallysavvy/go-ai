package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestCompletionModelDoGenerateMatchesTypeScriptRequestAndResponse(t *testing.T) {
	var capturedBody map[string]interface{}
	var capturedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/completions" {
			t.Fatalf("path = %q, want /completions", r.URL.Path)
		}
		capturedAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Test-Header", "test-value")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"cmpl_1",
			"created":1711363706,
			"model":"gpt-3.5-turbo-instruct",
			"choices":[{"text":"Hello, World!","finish_reason":"stop","logprobs":{"tokens":["Hello"],"token_logprobs":[-0.1]}}],
			"usage":{"prompt_tokens":4,"completion_tokens":30,"total_tokens":34}
		}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.CompletionModel("gpt-3.5-turbo-instruct")
	if err != nil {
		t.Fatalf("CompletionModel: %v", err)
	}
	if model.Provider() != "openai.completion" || model.SpecificationVersion() != "v4" {
		t.Fatalf("provider/spec = %q/%q", model.Provider(), model.SpecificationVersion())
	}

	max := 32
	temp := 0.7
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:      types.Prompt{Text: "Hello"},
		MaxTokens:   &max,
		Temperature: &temp,
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"logprobs": true,
				"user":     "user-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}

	if capturedAuth != "Bearer test-key" {
		t.Fatalf("Authorization = %q", capturedAuth)
	}
	if capturedBody["model"] != "gpt-3.5-turbo-instruct" {
		t.Fatalf("model = %#v", capturedBody["model"])
	}
	if capturedBody["prompt"] != "user:\nHello\n\nassistant:\n" {
		t.Fatalf("prompt = %#v", capturedBody["prompt"])
	}
	stop := capturedBody["stop"].([]interface{})
	if len(stop) != 1 || stop[0] != "\nuser:" {
		t.Fatalf("stop = %#v", stop)
	}
	if capturedBody["logprobs"] != float64(0) {
		t.Fatalf("logprobs = %#v, want 0", capturedBody["logprobs"])
	}
	if capturedBody["user"] != "user-1" {
		t.Fatalf("user = %#v", capturedBody["user"])
	}

	if result.Text != "Hello, World!" {
		t.Fatalf("Text = %q", result.Text)
	}
	if result.FinishReason != types.FinishReasonStop {
		t.Fatalf("FinishReason = %q", result.FinishReason)
	}
	if result.Usage.InputTokens == nil || *result.Usage.InputTokens != 4 {
		t.Fatalf("usage = %#v", result.Usage)
	}
	if result.ResponseMetadata == nil || result.ResponseMetadata.ID != "cmpl_1" || result.ResponseMetadata.ModelID != "gpt-3.5-turbo-instruct" {
		t.Fatalf("response metadata = %#v", result.ResponseMetadata)
	}
	meta := result.ProviderMetadata["openai"].(map[string]interface{})
	if _, ok := meta["logprobs"]; !ok {
		t.Fatalf("provider metadata missing logprobs: %#v", result.ProviderMetadata)
	}
}

func TestCompletionModelWarningsAndPromptErrorsMatchTypeScript(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewCompletionModel(p, "gpt-3.5-turbo-instruct")

	topK := 1
	body, warnings, err := model.buildCompletionRequest(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "Hello"},
		TopK:           &topK,
		Tools:          []types.Tool{{Name: "tool"}},
		ToolChoice:     types.AutoToolChoice(),
		ResponseFormat: &provider.ResponseFormat{Type: "json"},
	}, false)
	if err != nil {
		t.Fatalf("buildCompletionRequest: %v", err)
	}
	if body["prompt"] != "user:\nHello\n\nassistant:\n" {
		t.Fatalf("prompt = %#v", body["prompt"])
	}
	if len(warnings) != 4 {
		t.Fatalf("warnings = %#v, want 4 unsupported warnings", warnings)
	}

	_, _, err = convertToCompletionPrompt(types.Prompt{
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "first"}}},
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "late"}}},
		},
	})
	if err == nil {
		t.Fatal("expected unexpected system message error")
	}

	_, _, err = convertToCompletionPrompt(types.Prompt{
		Messages: []types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "call-1", ToolName: "lookup"}}},
		},
	})
	if err == nil {
		t.Fatal("expected tool-call messages error")
	}
}

func TestCompletionModelDoStreamMatchesTypeScriptChunkOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/completions" {
			t.Fatalf("path = %q, want /completions", r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["stream"] != true {
			t.Fatalf("stream = %#v, want true", body["stream"])
		}
		if _, ok := body["stream_options"].(map[string]interface{}); !ok {
			t.Fatalf("stream_options missing: %#v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"cmpl_1","created":1711363440,"model":"gpt-3.5-turbo-instruct","choices":[{"text":"Hello","index":0,"logprobs":null,"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"cmpl_1","created":1711363440,"model":"gpt-3.5-turbo-instruct","choices":[{"text":" world","index":0,"logprobs":null,"finish_reason":"stop"}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"cmpl_1","created":1711363440,"model":"gpt-3.5-turbo-instruct","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":2,"total_tokens":4}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewCompletionModel(p, "gpt-3.5-turbo-instruct")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
	})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	first, err := stream.Next()
	if err != nil {
		t.Fatalf("stream-start: %v", err)
	}
	if first.Type != provider.ChunkTypeStreamStart {
		t.Fatalf("first chunk = %q, want stream-start", first.Type)
	}
	meta, err := stream.Next()
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if meta.Type != provider.ChunkTypeResponseMetadata || meta.ResponseMetadata == nil || meta.ResponseMetadata.ID != "cmpl_1" {
		t.Fatalf("metadata chunk = %#v", meta)
	}
	textStart, err := stream.Next()
	if err != nil {
		t.Fatalf("text-start: %v", err)
	}
	if textStart.Type != provider.ChunkTypeTextStart || textStart.ID != "0" {
		t.Fatalf("text start = %#v", textStart)
	}
	text1, err := stream.Next()
	if err != nil {
		t.Fatalf("text1: %v", err)
	}
	text2, err := stream.Next()
	if err != nil {
		t.Fatalf("text2: %v", err)
	}
	if text1.Text+text2.Text != "Hello world" {
		t.Fatalf("text = %q + %q", text1.Text, text2.Text)
	}
	textEnd, err := stream.Next()
	if err != nil {
		t.Fatalf("text-end: %v", err)
	}
	if textEnd.Type != provider.ChunkTypeTextEnd || textEnd.ID != "0" {
		t.Fatalf("text end = %#v", textEnd)
	}
	finish, err := stream.Next()
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if finish.Type != provider.ChunkTypeFinish || finish.FinishReason != types.FinishReasonStop {
		t.Fatalf("finish = %#v", finish)
	}
	if finish.Usage == nil || finish.Usage.TotalTokens == nil || *finish.Usage.TotalTokens != 4 {
		t.Fatalf("finish usage = %#v", finish.Usage)
	}
	if _, err := stream.Next(); err != io.EOF {
		t.Fatalf("final err = %v, want EOF", err)
	}
}

func TestCompletionModelDoStreamFinishesWhenProviderClosesWithoutDone(t *testing.T) {
	stream := newCompletionStream(io.NopCloser(strings.NewReader(
		`data: {"id":"cmpl_1","model":"gpt-3.5-turbo-instruct","choices":[{"text":"hi","index":0,"finish_reason":"stop"}]}`+"\n\n",
	)), false)
	defer stream.Close() //nolint:errcheck

	for {
		chunk, err := stream.Next()
		if err != nil {
			t.Fatalf("stream ended before finish: %v", err)
		}
		if chunk.Type == provider.ChunkTypeFinish {
			if chunk.FinishReason != types.FinishReasonStop {
				t.Fatalf("finish reason = %q", chunk.FinishReason)
			}
			break
		}
	}
	if _, err := stream.Next(); err != io.EOF {
		t.Fatalf("final err = %v, want EOF", err)
	}
}

func TestCompletionModelDoStreamEarlyErrorReturnsError(t *testing.T) {
	stream := newCompletionStream(io.NopCloser(strings.NewReader(
		`data: {"error":{"message":"boom"}}`+"\n\n",
	)), false)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err == nil {
		t.Fatalf("stream.Next error = nil, chunk = %#v", chunk)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want boom", err)
	}
}

func TestCompletionModelDoStreamMetadataOnlyThenErrorIsStillEarly(t *testing.T) {
	stream := newCompletionStream(io.NopCloser(strings.NewReader(
		`data: {"id":"cmpl_1","model":"gpt-3.5-turbo-instruct","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`+"\n\n"+
			`data: {"error":{"message":"rate limited","type":"rate_limit_exceeded","param":null,"code":null}}`+"\n\n",
	)), false)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err == nil {
		t.Fatalf("stream.Next error = nil, chunk = %#v", chunk)
	}
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T %[1]v, want ProviderError", err)
	}
	if providerErr.StatusCode != 429 || providerErr.Message != "rate limited" {
		t.Fatalf("provider error = %#v", providerErr)
	}
}
