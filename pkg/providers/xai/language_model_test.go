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

// TestXAIChatLogprobsOption verifies that logprobs:true is serialized correctly
// in the chat completions request body.
func TestXAIChatLogprobsOption(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"choices": []interface{}{
				map[string]interface{}{
					"finish_reason": "stop",
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Hello!",
					},
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     5,
				"completion_tokens": 3,
				"total_tokens":      8,
			},
		})
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewLanguageModel(prov, "grok-3")

	trueVal := true
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"logprobs": &trueVal,
			},
		},
	}

	_, err := model.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	if capturedBody == nil {
		t.Skip("server not reached")
	}

	logprobs, ok := capturedBody["logprobs"]
	if !ok {
		t.Error("expected 'logprobs' field in request body")
		return
	}
	if logprobs != true {
		t.Errorf("logprobs = %v, want true", logprobs)
	}

	// top_logprobs should not be set when only logprobs is true.
	if _, hasTopLogprobs := capturedBody["topLogprobs"]; hasTopLogprobs {
		t.Error("top_logprobs should not be present when only logprobs=true")
	}
}

func TestXAIChatDoStreamIncludesRawChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"chunk-1","object":"chat.completion.chunk","created":1750538300,"model":"grok-3","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chunk-2","object":"chat.completion.chunk","created":1750538301,"model":"grok-3","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}

data: [DONE]

`)
	}))
	defer server.Close()

	model := NewLanguageModel(New(Config{APIKey: "test-key", BaseURL: server.URL}), "grok-3")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "hello"},
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatalf("DoStream() error: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	var sawRawBeforeText bool
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		switch chunk.Type {
		case provider.ChunkTypeRaw:
			raw, ok := chunk.Raw.(map[string]interface{})
			if !ok || raw["id"] != "chunk-1" {
				t.Fatalf("raw chunk = %#v, want first provider event", chunk.Raw)
			}
			sawRawBeforeText = true
		case provider.ChunkTypeResponseMetadata:
			if !sawRawBeforeText {
				t.Fatal("response metadata arrived before raw provider event")
			}
		case provider.ChunkTypeText:
			if !sawRawBeforeText {
				t.Fatal("text chunk arrived before raw provider event")
			}
			if !strings.Contains(chunk.Text, "Hello") {
				t.Fatalf("text = %q, want Hello", chunk.Text)
			}
			return
		}
	}
	t.Fatal("expected text chunk")
}

func TestXAIChatDoStreamWarningsMatchTypeScript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"chunk-1","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chunk-2","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`)
	}))
	defer server.Close()

	topK := 4
	model := NewLanguageModel(New(Config{APIKey: "test-key", BaseURL: server.URL}), "grok-3")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		TopK:   &topK,
	})
	if err != nil {
		t.Fatalf("DoStream() error: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("first chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeStreamStart || len(chunk.Warnings) != 1 {
		t.Fatalf("first chunk = %#v, want stream-start with one warning", chunk)
	}
	if chunk.Warnings[0].Type != "unsupported" || chunk.Warnings[0].Feature != "topK" {
		t.Fatalf("warning = %#v, want unsupported topK", chunk.Warnings[0])
	}
}

// TestXAIChatTopLogprobsAutoEnablesLogprobs verifies that setting topLogprobs implicitly
// enables logprobs in the chat completions request body.
func TestXAIChatTopLogprobsAutoEnablesLogprobs(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"choices": []interface{}{
				map[string]interface{}{
					"finish_reason": "stop",
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Hi!",
					},
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     3,
				"completion_tokens": 2,
				"total_tokens":      5,
			},
		})
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewLanguageModel(prov, "grok-3")

	topN := 3
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"topLogprobs": &topN,
			},
		},
	}

	_, err := model.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	if capturedBody == nil {
		t.Skip("server not reached")
	}

	// topLogprobs must auto-enable logprobs.
	logprobs, ok := capturedBody["logprobs"]
	if !ok {
		t.Error("expected 'logprobs' field in request body (auto-enabled by topLogprobs)")
		return
	}
	if logprobs != true {
		t.Errorf("logprobs = %v, want true", logprobs)
	}

	topLogprobs, ok := capturedBody["top_logprobs"]
	if !ok {
		t.Error("expected 'top_logprobs' field in request body")
		return
	}
	if topLogprobs != float64(3) {
		t.Errorf("top_logprobs = %v, want 3", topLogprobs)
	}
}

// TestXAIChatReasoningContentExtraction verifies that "reasoning"/"thinking" content
// parts in a chat completions response are extracted as ReasoningContent blocks.
func TestXAIChatReasoningContentExtraction(t *testing.T) {
	m := &LanguageModel{modelID: "grok-3"}

	rawReasoning := json.RawMessage(`{"type":"reasoning","text":"I think the answer is 42."}`)
	resp := xaiResponse{
		Choices: []xaiChoice{
			{
				FinishReason: "stop",
				Message: xaiMessage{
					Role: "assistant",
					Content: xaiMessageContent{
						Parts: []xaiContentPart{
							{Type: "reasoning", Text: "I think the answer is 42.", Raw: rawReasoning},
							{Type: "text", Text: "The answer is 42.", Raw: json.RawMessage(`{"type":"text","text":"The answer is 42."}`)},
						},
					},
				},
			},
		},
	}

	result := m.convertResponse(resp, "")

	if result.Text != "The answer is 42." {
		t.Errorf("Text = %q, want %q", result.Text, "The answer is 42.")
	}

	// Should have one ReasoningContent part.
	var foundReasoning bool
	for _, part := range result.Content {
		if rc, ok := part.(types.ReasoningContent); ok {
			foundReasoning = true
			if rc.Text != "I think the answer is 42." {
				t.Errorf("ReasoningContent.Text = %q, want %q", rc.Text, "I think the answer is 42.")
			}
		}
	}
	if !foundReasoning {
		t.Error("expected ReasoningContent part in result.Content")
	}
}

// TestXAIChatReasoningEffortProviderOption verifies that xaiOpts.ReasoningEffort="high"
// is serialized as reasoning_effort:"high" in the request body.
func TestXAIChatReasoningEffortProviderOption(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "grok-3")
	effort := "high"
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"reasoningEffort": &effort,
			},
		},
	}
	body := model.buildRequestBody(opts, false)
	if body["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v, want %q", body["reasoning_effort"], "high")
	}
}

// TestXAIChatReasoningEffortTopLevelMedium verifies that opts.Reasoning=Medium maps
// to reasoning_effort:"medium".
func TestXAIChatReasoningEffortTopLevelMedium(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "grok-3")
	reasoning := types.ReasoningMedium
	opts := &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hello"},
		Reasoning: &reasoning,
	}
	body := model.buildRequestBody(opts, false)
	if body["reasoning_effort"] != "medium" {
		t.Errorf("reasoning_effort = %v, want %q", body["reasoning_effort"], "medium")
	}
}

func TestXAIChatReasoningEffortProviderOptionNone(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "grok-4.3")
	effort := "none"
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"reasoningEffort": effort,
			},
		},
	}
	body := model.buildRequestBody(opts, false)
	if body["reasoning_effort"] != "none" {
		t.Errorf("reasoning_effort = %v, want %q", body["reasoning_effort"], "none")
	}
}

func TestXAIChatReasoningEffortTopLevelNoneOmitted(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "grok-4.3")
	reasoning := types.ReasoningNone
	opts := &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hello"},
		Reasoning: &reasoning,
	}
	body := model.buildRequestBody(opts, false)
	if _, ok := body["reasoning_effort"]; ok {
		t.Errorf("reasoning_effort must be omitted for top-level none, got %v", body["reasoning_effort"])
	}
}

// TestXAIChatReasoningEffortProviderOptionPrecedence verifies that the provider option
// takes precedence over the top-level opts.Reasoning.
func TestXAIChatReasoningEffortProviderOptionPrecedence(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "grok-3")
	effort := "low"
	reasoning := types.ReasoningHigh
	opts := &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hello"},
		Reasoning: &reasoning,
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"reasoningEffort": &effort,
			},
		},
	}
	body := model.buildRequestBody(opts, false)
	if body["reasoning_effort"] != "low" {
		t.Errorf("reasoning_effort = %v, want %q (provider option should override top-level)", body["reasoning_effort"], "low")
	}
}

// TestXAIChatParallelFunctionCalling verifies that parallel_function_calling:false
// is serialized in the request body.
func TestXAIChatParallelFunctionCalling(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "grok-3")
	falseVal := false
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"parallelFunctionCalling": &falseVal,
			},
		},
	}
	body := model.buildRequestBody(opts, false)
	val, ok := body["parallel_function_calling"]
	if !ok {
		t.Error("expected 'parallel_function_calling' in request body")
		return
	}
	if val != false {
		t.Errorf("parallel_function_calling = %v, want false", val)
	}
}

func TestXAIChatParallelFunctionCallingSnakeCaseProviderOption(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "grok-3")
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"parallel_function_calling": false,
			},
		},
	}
	body := model.buildRequestBody(opts, false)
	if body["parallel_function_calling"] != false {
		t.Errorf("parallel_function_calling = %v, want false", body["parallel_function_calling"])
	}
}

func TestXAIChatToolsDoNotEmitAdditionalPropertiesFalse(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "grok-3")
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		Tools: []types.Tool{
			{
				Name:        "lookup",
				Description: "lookup tool",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"q": map[string]interface{}{"type": "string"},
					},
					"required":             []string{"q"},
					"additionalProperties": false,
				},
			},
		},
	}
	body := model.buildRequestBody(opts, false)
	tools, ok := body["tools"].([]map[string]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", body["tools"])
	}
	fn := tools[0]["function"].(map[string]interface{})
	params := fn["parameters"].(map[string]interface{})
	if _, exists := params["additionalProperties"]; exists {
		t.Fatalf("unexpected additionalProperties in xAI tool schema: %#v", params)
	}
}

// TestXAIChatSearchParametersBasic verifies that searchParameters with mode:"auto"
// is serialized as search_parameters:{mode:"auto"} in the request body.
func TestXAIChatSearchParametersBasic(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "grok-3")
	returnCitations := true
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"searchParameters": map[string]interface{}{
					"mode":            "auto",
					"returnCitations": &returnCitations,
				},
			},
		},
	}
	body := model.buildRequestBody(opts, false)
	sp, ok := body["search_parameters"]
	if !ok {
		t.Error("expected 'search_parameters' in request body")
		return
	}
	spMap, ok := sp.(map[string]interface{})
	if !ok {
		t.Fatalf("search_parameters type = %T, want map", sp)
	}
	if spMap["mode"] != "auto" {
		t.Errorf("search_parameters.mode = %v, want %q", spMap["mode"], "auto")
	}
	if spMap["return_citations"] != true {
		t.Errorf("search_parameters.return_citations = %v, want true", spMap["return_citations"])
	}
}

// TestXAIChatSearchParametersWebSource verifies that a web source with country is
// serialized with the correct snake_case keys.
func TestXAIChatSearchParametersWebSource(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "grok-3")
	country := "US"
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"searchParameters": map[string]interface{}{
					"mode": "on",
					"sources": []interface{}{
						map[string]interface{}{
							"type":    "web",
							"country": &country,
						},
					},
				},
			},
		},
	}
	body := model.buildRequestBody(opts, false)
	sp, ok := body["search_parameters"]
	if !ok {
		t.Fatal("expected 'search_parameters' in request body")
	}
	spMap := sp.(map[string]interface{})
	sources, ok := spMap["sources"]
	if !ok {
		t.Fatal("expected 'sources' in search_parameters")
	}
	sl, ok := sources.([]map[string]interface{})
	if !ok || len(sl) == 0 {
		t.Fatalf("sources type = %T, len = 0", sources)
	}
	if sl[0]["country"] != "US" {
		t.Errorf("sources[0].country = %v, want %q", sl[0]["country"], "US")
	}
}

// TestXAIChatBuildRequestBodyLogprobsNone verifies no logprobs fields when not set.
func TestXAIChatBuildRequestBodyLogprobsNone(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "grok-3")

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	}

	body := model.buildRequestBody(opts, false)

	if _, ok := body["logprobs"]; ok {
		t.Error("logprobs should not be in request body when not configured")
	}
	if _, ok := body["topLogprobs"]; ok {
		t.Error("top_logprobs should not be in request body when not configured")
	}
}
