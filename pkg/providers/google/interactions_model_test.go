package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestInteractionsGenerateRequestAndResponse(t *testing.T) {
	t.Parallel()

	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/interactions" {
			t.Fatalf("path = %q, want /interactions", r.URL.Path)
		}
		if got := r.Header.Get("Api-Revision"); got != "" {
			t.Fatalf("Api-Revision header = %q, want empty", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("X-Gemini-Service-Tier", "priority")
		_, _ = fmt.Fprint(w, `{
			"id":"v1_test",
			"status":"completed",
			"model":"gemini-2.5-flash",
			"created":"2026-05-04T19:00:00Z",
			"steps":[
				{"type":"thought","signature":"sig-1","summary":[{"type":"text","text":"thinking"}]},
				{"type":"text","text":"hello"},
				{"type":"function_call","id":"call-1","name":"lookup","arguments":{"q":"x"},"signature":"sig-2"}
			],
			"usage":{"total_input_tokens":3,"total_output_tokens":4,"total_tokens":7,"total_thought_tokens":2}
		}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.Interactions(ModelGemini25Flash)
	if err != nil {
		t.Fatalf("Interactions: %v", err)
	}
	temp := 0.2
	maxTokens := 64
	store := false
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			System: "system text",
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{
					types.TextContent{Text: "describe"},
					types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/a.png", MediaType: "image/png"}},
				}},
			},
		},
		Temperature: &temp,
		MaxTokens:   &maxTokens,
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json",
			Schema: map[string]interface{}{"type": "object"},
		},
		Tools:      []types.Tool{{Name: "lookup", Description: "Lookup", Parameters: map[string]interface{}{"type": "object"}}},
		ToolChoice: types.SpecificToolChoice("lookup"),
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{
			"store":              store,
			"mediaResolution":    "high",
			"responseModalities": []interface{}{"text", "image"},
			"serviceTier":        "priority",
			"thinkingLevel":      "low",
		}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}

	if captured["model"] != ModelGemini25Flash {
		t.Fatalf("model = %v", captured["model"])
	}
	if captured["system_instruction"] != "system text" {
		t.Fatalf("system_instruction = %v", captured["system_instruction"])
	}
	responseFormat, ok := captured["response_format"].([]interface{})
	if !ok || len(responseFormat) != 1 {
		t.Fatalf("response_format = %v, want a single text/application-json entry", captured["response_format"])
	}
	entry := responseFormat[0].(map[string]interface{})
	if entry["type"] != "text" || entry["mime_type"] != "application/json" {
		t.Fatalf("response_format[0] = %v", entry)
	}
	generationConfig := captured["generation_config"].(map[string]interface{})
	if generationConfig["max_output_tokens"] != float64(64) {
		t.Fatalf("max_output_tokens = %v", generationConfig["max_output_tokens"])
	}
	if generationConfig["thinking_level"] != "low" {
		t.Fatalf("thinking_level = %v", generationConfig["thinking_level"])
	}
	if _, ok := generationConfig["tool_choice"].(map[string]interface{}); !ok {
		t.Fatalf("tool_choice missing from generation_config: %#v", generationConfig)
	}
	// The whole prompt is a single user message, so it becomes one top-level
	// `user_input` step (never collapsed to a bare content array — TS
	// `GoogleInteractionsInput` is always `Array<Step>`).
	input := captured["input"].([]interface{})
	if len(input) != 1 {
		t.Fatalf("input len = %d, want 1 user_input step: %#v", len(input), input)
	}
	step0 := input[0].(map[string]interface{})
	if step0["type"] != "user_input" {
		t.Fatalf("input[0] = %#v", step0)
	}
	stepContent := step0["content"].([]interface{})
	fileBlock := stepContent[1].(map[string]interface{})
	if fileBlock["type"] != "image" || fileBlock["uri"] != "https://example.com/a.png" || fileBlock["resolution"] != "high" {
		t.Fatalf("file block = %#v", fileBlock)
	}
	if result.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("finish reason = %v", result.FinishReason)
	}
	if result.Text != "hello" {
		t.Fatalf("text = %q", result.Text)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ThoughtSignature != "sig-2" {
		t.Fatalf("tool calls = %#v", result.ToolCalls)
	}
	if result.ProviderMetadata["google"].(map[string]interface{})["interactionId"] != "v1_test" {
		t.Fatalf("provider metadata = %#v", result.ProviderMetadata)
	}
	if result.ResponseMetadata == nil || result.ResponseMetadata.ModelID != "gemini-2.5-flash" || result.ResponseMetadata.ID != "v1_test" {
		t.Fatalf("response metadata = %#v", result.ResponseMetadata)
	}
	if result.ResponseMetadata.Timestamp.IsZero() {
		t.Fatalf("response timestamp must be parsed from created")
	}
	if body, ok := result.ResponseMetadata.Body.(json.RawMessage); !ok || !strings.Contains(string(body), `"id":"v1_test"`) {
		t.Fatalf("response body = %#v, want raw body", result.ResponseMetadata.Body)
	}
}

func TestInteractionsPreviousInteractionCompactsAssistantAndToolResult(t *testing.T) {
	t.Parallel()

	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = fmt.Fprint(w, `{"id":"v1_next","status":"completed","steps":[{"type":"text","text":"next"}]}`)
	}))
	defer server.Close()

	meta := providerMetaRaw("", "v1_prev")
	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.Interactions(ModelGemini25Flash)
	if err != nil {
		t.Fatalf("Interactions: %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "first"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "old", ProviderMetadata: meta}}, ToolCalls: []types.ToolCall{{ID: "call-old", ToolName: "lookup"}}},
			{Role: types.RoleTool, Content: []types.ContentPart{types.SimpleTextResult("call-old", "lookup", "old result")}},
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "next"}}},
		}},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"previousInteractionId": "v1_prev"}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if captured["previous_interaction_id"] != "v1_prev" {
		t.Fatalf("previous_interaction_id = %v", captured["previous_interaction_id"])
	}
	// Compaction drops whole assistant/tool messages, so only the two user
	// messages ("first", "next") should remain, each its own `user_input`
	// step — no `function_call`/`thought`/`model_output` step (which would
	// only come from an assistant message) should have leaked through.
	steps := captured["input"].([]interface{})
	if len(steps) != 2 {
		t.Fatalf("step count = %d, body=%#v", len(steps), captured["input"])
	}
	for _, step := range steps {
		if step.(map[string]interface{})["type"] != "user_input" {
			t.Fatalf("assistant/tool turn was not compacted: %#v", steps)
		}
	}
}

func TestInteractionsAgentPollingTimeoutCancelsInteraction(t *testing.T) {
	t.Parallel()

	cancelCalled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/interactions":
			_, _ = fmt.Fprint(w, `{"id":"v1_agent","status":"in_progress"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/interactions/v1_agent":
			_, _ = fmt.Fprint(w, `{"id":"v1_agent","status":"in_progress"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/interactions/v1_agent/cancel":
			cancelCalled <- struct{}{}
			_, _ = fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.InteractionsAgent(InteractionsAgentDeepResearch)
	if err != nil {
		t.Fatalf("InteractionsAgent: %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "research"},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"pollingTimeoutMs": 5}},
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	select {
	case <-cancelCalled:
	case <-time.After(time.Second):
		t.Fatal("expected cancel endpoint to be called")
	}
}

func TestInteractionsRequiresActionIsNotPollingTerminal(t *testing.T) {
	t.Parallel()

	if isTerminalInteractionStatus(InteractionStatusRequiresAction) {
		t.Fatal("requires_action must not be terminal for agent polling; TS continues polling until completed/failed/cancelled/incomplete")
	}
}

func TestInteractionsAgentUsesCurrentDeepResearchAgentName(t *testing.T) {
	t.Parallel()

	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/interactions" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = fmt.Fprint(w, `{"id":"v1_agent","status":"completed","steps":[{"type":"text","text":"ok"}]}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.InteractionsAgent(InteractionsAgentDeepResearch)
	if err != nil {
		t.Fatalf("InteractionsAgent: %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "research"}}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if captured["agent"] != InteractionsAgentDeepResearchProPreview {
		t.Fatalf("agent = %v, want %s", captured["agent"], InteractionsAgentDeepResearchProPreview)
	}
	if captured["background"] != true {
		t.Fatalf("background = %v", captured["background"])
	}
}

func TestInteractionsTypedImageConfigMapsToSnakeCase(t *testing.T) {
	t.Parallel()

	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = fmt.Fprint(w, `{"id":"v1_img","status":"completed","steps":[{"type":"text","text":"ok"}]}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.Interactions(InteractionsModelGemini3ProImage)
	if err != nil {
		t.Fatalf("Interactions: %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "make image"},
		ProviderOptions: map[string]interface{}{
			"google": GoogleInteractionsProviderOptions{
				ResponseModalities: []string{"image"},
				ImageConfig: map[string]interface{}{
					"aspectRatio": "16:9",
					"imageSize":   "2K",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	// The deprecated imageConfig option now contributes a response_format
	// image entry (TS: the legacy imageConfig fallback), not
	// generation_config.image_config (which does not exist on the wire).
	if _, ok := captured["generation_config"]; ok {
		if gc, ok := captured["generation_config"].(map[string]interface{}); ok {
			if _, has := gc["image_config"]; has {
				t.Fatalf("generation_config.image_config should not be sent: %#v", gc)
			}
		}
	}
	responseFormat, ok := captured["response_format"].([]interface{})
	if !ok || len(responseFormat) != 1 {
		t.Fatalf("response_format = %v, want a single image entry", captured["response_format"])
	}
	entry := responseFormat[0].(map[string]interface{})
	if entry["type"] != "image" || entry["aspect_ratio"] != "16:9" || entry["image_size"] != "2K" {
		t.Fatalf("response_format[0] = %#v", entry)
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "imageConfig is deprecated") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a deprecation warning for imageConfig, got %+v", result.Warnings)
	}
}

// TestInteractionsResponseFormatVideoEntry ports TS
// google-interactions-language-model.ts's `response_format` video entry
// (ca29e9b): providerOptions.google.responseFormat entries of type "video"
// map to a {type:'video', aspect_ratio, resolution, duration, delivery,
// gcs_uri} wire entry from their camelCase options, and (unlike the deprecated
// imageConfig fallback) are sent for both model and agent calls.
func TestInteractionsResponseFormatVideoEntry(t *testing.T) {
	t.Parallel()

	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = fmt.Fprint(w, `{"id":"v1_vid","status":"completed","steps":[{"type":"text","text":"ok"}]}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.Interactions(ModelGemini25Flash)
	if err != nil {
		t.Fatalf("Interactions: %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "make a video"},
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"responseFormat": []interface{}{
					map[string]interface{}{
						"type":        "video",
						"aspectRatio": "16:9",
						"resolution":  "1080p",
						"duration":    "8s",
						"delivery":    "uri",
						"gcsUri":      "gs://my-bucket/out.mp4",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	responseFormat, ok := captured["response_format"].([]interface{})
	if !ok || len(responseFormat) != 1 {
		t.Fatalf("response_format = %v, want a single video entry", captured["response_format"])
	}
	entry := responseFormat[0].(map[string]interface{})
	want := map[string]interface{}{
		"type":         "video",
		"aspect_ratio": "16:9",
		"resolution":   "1080p",
		"duration":     "8s",
		"delivery":     "uri",
		"gcs_uri":      "gs://my-bucket/out.mp4",
	}
	for k, v := range want {
		if entry[k] != v {
			t.Fatalf("response_format[0][%q] = %v, want %v (entry=%#v)", k, entry[k], v, entry)
		}
	}
}

// TestInteractionsResponseFormatVideoEntry_AgentKeepsIt verifies the video
// (and any other) responseFormat entry is still sent when an agent is set —
// only the AI SDK call-level JSON responseFormat and generation_config are
// agent-excluded, not providerOptions.google.responseFormat.
func TestInteractionsResponseFormatVideoEntry_AgentKeepsIt(t *testing.T) {
	t.Parallel()

	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = fmt.Fprint(w, `{"id":"v1_agent_vid","status":"completed","steps":[{"type":"text","text":"ok"}]}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.InteractionsAgent(InteractionsAgentDeepResearchProPreview)
	if err != nil {
		t.Fatalf("InteractionsAgent: %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "research and render a video"},
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"responseFormat": []interface{}{
					map[string]interface{}{"type": "video", "aspectRatio": "9:16"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	responseFormat, ok := captured["response_format"].([]interface{})
	if !ok || len(responseFormat) != 1 {
		t.Fatalf("response_format = %v, want the video entry to survive on an agent call", captured["response_format"])
	}
	entry := responseFormat[0].(map[string]interface{})
	if entry["type"] != "video" || entry["aspect_ratio"] != "9:16" {
		t.Fatalf("response_format[0] = %#v", entry)
	}
}

func TestInteractionsBuiltinToolResultsEmitSources(t *testing.T) {
	t.Parallel()

	p := New(Config{APIKey: "test-key"})
	model := NewInteractionsLanguageModel(p, ModelGemini25Flash)
	content, _, _ := model.parseOutputs([]interactionsContentBlock{
		{
			Type:   "google_search_result",
			CallID: "search-1",
			Result: []interface{}{
				map[string]interface{}{"url": "https://example.com/a", "title": "A"},
			},
		},
		{
			Type:   "file_search_result",
			CallID: "file-1",
			Result: []interface{}{
				map[string]interface{}{"file_name": "report.pdf", "document_uri": "gs://bucket/report.pdf", "title": "Report"},
			},
		},
	}, "v1_sources")

	var urlSource, docSource bool
	for _, part := range content {
		source, ok := part.(types.SourceContent)
		if !ok {
			continue
		}
		if source.SourceType == "url" && source.URL == "https://example.com/a" && source.Title == "A" {
			urlSource = true
		}
		if source.SourceType == "document" && source.MediaType == "application/pdf" && source.Filename == "report.pdf" {
			docSource = true
		}
	}
	if !urlSource || !docSource {
		t.Fatalf("expected url and document sources, got %#v", content)
	}
}

func TestInteractionsParseOutputsPreservesToolCallsInOrderedContent(t *testing.T) {
	t.Parallel()

	p := New(Config{APIKey: "test-key"})
	model := NewInteractionsLanguageModel(p, ModelGemini25Flash)
	content, toolCalls, hasFunctionCall := model.parseOutputs([]interactionsContentBlock{
		{Type: "text", Text: "before"},
		{Type: "function_call", ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"q":"x"}`), Signature: "sig"},
		{Type: "text", Text: "after"},
		{Type: "google_search_call", ID: "search-1", Arguments: json.RawMessage(`{"query":"go"}`)},
	}, "v1_order")

	if !hasFunctionCall {
		t.Fatal("expected hasFunctionCall")
	}
	if len(toolCalls) != 2 {
		t.Fatalf("toolCalls len = %d, want 2", len(toolCalls))
	}
	if len(content) != 4 {
		t.Fatalf("content len = %d, want 4: %#v", len(content), content)
	}
	if _, ok := content[1].(types.ToolCallContent); !ok {
		t.Fatalf("content[1] = %T, want ToolCallContent", content[1])
	}
	builtin, ok := content[3].(types.ToolCallContent)
	if !ok {
		t.Fatalf("content[3] = %T, want ToolCallContent", content[3])
	}
	if !builtin.ProviderExecuted || builtin.ToolName != "google_search" {
		t.Fatalf("builtin tool call content = %#v", builtin)
	}
}

func TestInteractionsAssistantToolCallContentRoundTrips(t *testing.T) {
	t.Parallel()

	model := NewInteractionsLanguageModel(New(Config{APIKey: "test-key"}), ModelGemini25Flash)
	steps, warnings, err := model.convertAssistantSteps([]types.ContentPart{
		types.TextContent{Text: "before"},
		types.ToolCallContent{
			ToolCallID:       "call-1",
			ToolName:         "lookup",
			Input:            `{"q":"x"}`,
			ProviderMetadata: providerMetaRaw("sig-1", "v1_prev"),
		},
	}, nil, "")
	if err != nil {
		t.Fatalf("convertAssistantSteps() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	// Text before the tool-call flushes as its own top-level model_output
	// step; the tool-call becomes a separate top-level function_call step —
	// never nested content inside one combined block (TS: adjacent text/file
	// coalesces into model_output, but a tool-call always flushes it first).
	if len(steps) != 2 {
		t.Fatalf("steps len = %d, want 2: %#v", len(steps), steps)
	}
	if steps[0]["type"] != "model_output" {
		t.Fatalf("steps[0] = %#v", steps[0])
	}
	modelOutputContent := steps[0]["content"].([]map[string]interface{})
	if len(modelOutputContent) != 1 || modelOutputContent[0]["text"] != "before" {
		t.Fatalf("steps[0].content = %#v", modelOutputContent)
	}
	call := steps[1]
	if call["type"] != "function_call" || call["id"] != "call-1" || call["name"] != "lookup" {
		t.Fatalf("function_call step = %#v", call)
	}
	if call["signature"] != "sig-1" {
		t.Fatalf("signature = %#v", call["signature"])
	}
	args := call["arguments"].(map[string]interface{})
	if args["q"] != "x" {
		t.Fatalf("arguments = %#v", args)
	}
}

func TestInteractionsImageURIOutputsAreGeneratedFiles(t *testing.T) {
	t.Parallel()

	model := NewInteractionsLanguageModel(New(Config{APIKey: "test-key"}), ModelGemini25Flash)
	content, _, _ := model.parseOutputs([]interactionsContentBlock{
		{Type: "image", URI: "https://example.com/out.png", MimeType: "image/png"},
	}, "v1_img")
	if len(content) != 1 {
		t.Fatalf("content len = %d", len(content))
	}
	file, ok := content[0].(types.GeneratedFileContent)
	if !ok {
		t.Fatalf("content[0] = %T, want GeneratedFileContent", content[0])
	}
	if file.FileData.Type != types.FileDataTypeURL || file.FileData.URL != "https://example.com/out.png" || file.URL != "https://example.com/out.png" {
		t.Fatalf("file url data = %#v", file)
	}
	if len(file.ProviderMetadata) == 0 {
		t.Fatal("expected provider metadata on generated file")
	}
}

// TestInteractionsVideoModelOutputBecomesGeneratedFile ports the dc1eb8d
// video-output row: a `video` content block nested inside a `model_output`
// step becomes a GeneratedFileContent part (TS parse-google-interactions-
// outputs.ts blockType === 'video').
func TestInteractionsVideoModelOutputBecomesGeneratedFile(t *testing.T) {
	t.Parallel()

	model := NewInteractionsLanguageModel(New(Config{APIKey: "test-key"}), ModelGemini25Flash)
	content, _, _ := model.parseOutputs([]interactionsContentBlock{
		{Type: "model_output", ContentRaw: json.RawMessage(`[{"type":"video","uri":"gs://bucket/out.mp4","mime_type":"video/mp4"}]`)},
	}, "v1_vid")
	if len(content) != 1 {
		t.Fatalf("content len = %d, content=%#v", len(content), content)
	}
	file, ok := content[0].(types.GeneratedFileContent)
	if !ok {
		t.Fatalf("content[0] = %T, want GeneratedFileContent", content[0])
	}
	if file.MediaType != "video/mp4" || file.URL != "gs://bucket/out.mp4" {
		t.Fatalf("file = %#v", file)
	}
}

// TestInteractionsProcessingCallAndResultParseAndReplay ports the 18ad19c
// agentic video processing row end to end: `processing_call`/
// `processing_result` steps parse into `google.processing_call`/
// `google.processing_result` custom content, and replaying those parts back
// into a request produces the matching `processing_call`/`processing_result`
// input steps (TS convert-to-google-interactions-input.ts).
func TestInteractionsProcessingCallAndResultParseAndReplay(t *testing.T) {
	t.Parallel()

	model := NewInteractionsLanguageModel(New(Config{APIKey: "test-key"}), ModelGemini25Flash)
	content, _, _ := model.parseOutputs([]interactionsContentBlock{
		{Type: "processing_call", ID: "proc-1", Signature: "sig-a"},
		{Type: "processing_result", CallID: "proc-1", Signature: "sig-b"},
	}, "v1_proc")
	if len(content) != 2 {
		t.Fatalf("content len = %d, content=%#v", len(content), content)
	}
	call, ok := content[0].(types.CustomContent)
	if !ok || call.Kind != "google.processing_call" {
		t.Fatalf("content[0] = %#v", content[0])
	}
	result, ok := content[1].(types.CustomContent)
	if !ok || result.Kind != "google.processing_result" {
		t.Fatalf("content[1] = %#v", content[1])
	}

	replayed, warnings, err := model.convertAssistantSteps([]types.ContentPart{call, result}, nil, "")
	if err != nil {
		t.Fatalf("convertAssistantSteps() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if len(replayed) != 2 {
		t.Fatalf("replayed len = %d, replayed=%#v", len(replayed), replayed)
	}
	if replayed[0]["type"] != "processing_call" || replayed[0]["id"] != "proc-1" || replayed[0]["signature"] != "sig-a" {
		t.Fatalf("replayed[0] = %#v", replayed[0])
	}
	if replayed[1]["type"] != "processing_result" || replayed[1]["call_id"] != "proc-1" || replayed[1]["signature"] != "sig-b" {
		t.Fatalf("replayed[1] = %#v", replayed[1])
	}
}

// TestInteractionsProcessingCustomContentInvalidKindWarns verifies an
// unrecognized/invalid "custom" content part is dropped with a warning
// rather than silently vanishing or panicking.
func TestInteractionsProcessingCustomContentInvalidKindWarns(t *testing.T) {
	t.Parallel()

	model := NewInteractionsLanguageModel(New(Config{APIKey: "test-key"}), ModelGemini25Flash)
	_, warnings, err := model.convertAssistantSteps([]types.ContentPart{
		types.CustomContent{Kind: "other.thing"},
	}, nil, "")
	if err != nil {
		t.Fatalf("convertAssistantSteps() error = %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "unsupported or invalid custom") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

// TestInteractionsVideoInputProcessingOption ports the video-options half of
// 18ad19c: providerOptions.google.processing on a video file part maps to
// the wire `processing` field ("agentic"/"static"/a static config), or emits
// a warning (block still sent) when invalid.
func TestInteractionsVideoInputProcessingOption(t *testing.T) {
	t.Parallel()

	t.Run("agentic string", func(t *testing.T) {
		block, warnings, err := fileContentToInteractionBlock(types.FileContent{
			MediaType: "video/mp4",
			URL:       "https://example.com/in.mp4",
			ProviderOptions: map[string]interface{}{
				"google": map[string]interface{}{"processing": "agentic"},
			},
		}, "")
		if err != nil || len(warnings) != 0 {
			t.Fatalf("err=%v warnings=%#v", err, warnings)
		}
		if block["processing"] != "agentic" {
			t.Fatalf("processing = %#v", block["processing"])
		}
	})

	t.Run("static config object", func(t *testing.T) {
		// start_offset/end_offset serialize as duration strings ("1s"/"5s"),
		// not raw numbers, mirroring TS `${config.startOffset}s` (ai@7.0.118
		// commit b126c4b222). fps stays numeric.
		block, warnings, err := fileContentToInteractionBlock(types.FileContent{
			MediaType: "video/mp4",
			URL:       "https://example.com/in.mp4",
			ProviderOptions: map[string]interface{}{
				"google": map[string]interface{}{
					"processing": map[string]interface{}{
						"type":        "static",
						"startOffset": float64(1),
						"endOffset":   float64(5),
						"fps":         float64(2),
					},
				},
			},
		}, "")
		if err != nil || len(warnings) != 0 {
			t.Fatalf("err=%v warnings=%#v", err, warnings)
		}
		processing, ok := block["processing"].(map[string]interface{})
		if !ok {
			t.Fatalf("processing = %#v", block["processing"])
		}
		if processing["type"] != "static" || processing["start_offset"] != "1s" || processing["end_offset"] != "5s" || processing["fps"] != float64(2) {
			t.Fatalf("processing = %#v", processing)
		}
	})

	t.Run("static config object with fractional offsets", func(t *testing.T) {
		block, warnings, err := fileContentToInteractionBlock(types.FileContent{
			MediaType: "video/mp4",
			URL:       "https://example.com/in.mp4",
			ProviderOptions: map[string]interface{}{
				"google": map[string]interface{}{
					"processing": map[string]interface{}{
						"type":        "static",
						"startOffset": 10.5,
						"endOffset":   20.25,
					},
				},
			},
		}, "")
		if err != nil || len(warnings) != 0 {
			t.Fatalf("err=%v warnings=%#v", err, warnings)
		}
		processing, ok := block["processing"].(map[string]interface{})
		if !ok {
			t.Fatalf("processing = %#v", block["processing"])
		}
		if processing["start_offset"] != "10.5s" || processing["end_offset"] != "20.25s" {
			t.Fatalf("processing = %#v", processing)
		}
		if _, has := processing["fps"]; has {
			t.Fatalf("expected no fps key when unset: %#v", processing)
		}
	})

	t.Run("invalid value warns but keeps the block", func(t *testing.T) {
		block, warnings, err := fileContentToInteractionBlock(types.FileContent{
			MediaType: "video/mp4",
			URL:       "https://example.com/in.mp4",
			ProviderOptions: map[string]interface{}{
				"google": map[string]interface{}{"processing": 42},
			},
		}, "")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if block == nil {
			t.Fatal("expected the video block to still be emitted")
		}
		if _, has := block["processing"]; has {
			t.Fatalf("processing should be dropped: %#v", block)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "invalid providerOptions.google.processing") {
			t.Fatalf("warnings = %#v", warnings)
		}
	})
}

// TestInteractionsOutputTokensByModality ports dc1eb8d's usage half:
// usage.output_tokens_by_modality surfaces as
// providerMetadata.google.outputTokensByModality on both the non-streaming
// and streaming finish paths.
func TestInteractionsOutputTokensByModality(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"id":"v1_mod","status":"completed","steps":[{"type":"text","text":"ok"}],"usage":{"output_tokens_by_modality":[{"modality":"video","tokens":57920},{"modality":"text","tokens":12}]}}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.Interactions(ModelGemini25Flash)
	if err != nil {
		t.Fatalf("Interactions: %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "make a video"}})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	googleMeta := result.ProviderMetadata["google"].(map[string]interface{})
	byModality, ok := googleMeta["outputTokensByModality"].(map[string]interface{})
	if !ok {
		t.Fatalf("outputTokensByModality = %#v", googleMeta["outputTokensByModality"])
	}
	if byModality["video"] != int64(57920) || byModality["text"] != int64(12) {
		t.Fatalf("outputTokensByModality = %#v", byModality)
	}
}

func TestInteractionsGenerateEmptyProviderMetadataGoogleObject(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"status":"completed","steps":[{"type":"text","text":"ok"}]}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.Interactions(ModelGemini25Flash)
	if err != nil {
		t.Fatalf("Interactions: %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	googleMeta, ok := result.ProviderMetadata["google"].(map[string]interface{})
	if !ok {
		t.Fatalf("google metadata = %#v", result.ProviderMetadata["google"])
	}
	if googleMeta == nil {
		t.Fatal("google metadata map is nil")
	}
}

func TestInteractionsAgentStreamReconnectsAfterIntermittentEOF(t *testing.T) {
	t.Parallel()

	getCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/interactions":
			_, _ = fmt.Fprint(w, `{"id":"v1_stream","status":"in_progress"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/interactions/v1_stream":
			getCount++
			w.Header().Set("Content-Type", "text/event-stream")
			if getCount == 1 {
				writeSSE(w, `{"event_type":"interaction.created","interaction":{"id":"v1_stream","status":"in_progress","model":"gemini-2.5-flash"}}`)
				writeSSE(w, `{"event_type":"step.start","index":0,"step":{"type":"model_output"}}`)
				writeSSE(w, `{"event_type":"step.delta","index":0,"delta":{"type":"text","text":"hel"}}`)
				return
			}
			writeSSE(w, `{"event_type":"step.delta","index":0,"delta":{"type":"text","text":"lo"}}`)
			writeSSE(w, `{"event_type":"step.stop","index":0}`)
			writeSSE(w, `{"event_type":"interaction.completed","interaction":{"id":"v1_stream","status":"completed","usage":{"total_tokens":2},"service_tier":"standard"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.InteractionsAgent(InteractionsAgentDeepResearch)
	if err != nil {
		t.Fatalf("InteractionsAgent: %v", err)
	}
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var text strings.Builder
	var finish *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next: %v", err)
		}
		if chunk.Type == provider.ChunkTypeText {
			text.WriteString(chunk.Text)
		}
		if chunk.Type == provider.ChunkTypeFinish {
			finish = chunk
		}
	}
	if text.String() != "hello" {
		t.Fatalf("text = %q", text.String())
	}
	if finish == nil || finish.FinishReason != types.FinishReasonStop {
		t.Fatalf("finish = %#v", finish)
	}
	if getCount != 2 {
		t.Fatalf("GET stream count = %d, want 2", getCount)
	}
}

func TestInteractionsStreamToolInputStartEmittedOnce(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"event_type":"interaction.created","interaction":{"id":"v1_stream","status":"in_progress","model":"gemini-2.5-flash"}}`,
		``,
		`data: {"event_type":"step.start","index":0,"step":{"type":"function_call","id":"call-1","name":"lookup"}}`,
		``,
		`data: {"event_type":"step.delta","index":0,"delta":{"type":"arguments_delta","arguments":"{\"q\":\"x\"}"}}`,
		``,
		`data: {"event_type":"step.stop","index":0}`,
		``,
		`data: {"event_type":"interaction.completed","interaction":{"id":"v1_stream","status":"completed"}}`,
		``,
	}, "\n")
	stream := newInteractionsEventStream(context.Background(), New(Config{APIKey: "test-key"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	defer func() { _ = stream.Close() }()

	var startCount int
	var call *types.ToolCall
	for {
		chunk, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next: %v", err)
		}
		if chunk.Type == provider.ChunkTypeToolInputStart {
			startCount++
		}
		if chunk.Type == provider.ChunkTypeToolCall {
			call = chunk.ToolCall
		}
	}
	if startCount != 1 {
		t.Fatalf("tool-input-start count = %d, want 1", startCount)
	}
	if call == nil || call.RawArguments != `{"q":"x"}` {
		t.Fatalf("tool call = %#v", call)
	}
}

func TestInteractionsStreamFlushesFinishWithoutComplete(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"event_type":"interaction.created","interaction":{"id":"v1_stream","status":"in_progress","model":"gemini-2.5-flash"}}`,
		``,
		`data: {"event_type":"interaction.status_update","status":"incomplete"}`,
		``,
		`data: {"event_type":"step.start","index":0,"step":{"type":"model_output"}}`,
		``,
		`data: {"event_type":"step.delta","index":0,"delta":{"type":"text","text":"partial"}}`,
		``,
	}, "\n")
	stream := newInteractionsEventStream(context.Background(), New(Config{APIKey: "test-key"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	defer func() { _ = stream.Close() }()

	var sawTextEnd bool
	var finish *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next: %v", err)
		}
		if chunk.Type == provider.ChunkTypeTextEnd {
			sawTextEnd = true
		}
		if chunk.Type == provider.ChunkTypeFinish {
			finish = chunk
		}
	}
	if !sawTextEnd {
		t.Fatal("expected open text block to be closed on EOF")
	}
	if finish == nil || finish.FinishReason != types.FinishReasonLength {
		t.Fatalf("finish = %#v", finish)
	}
}

func TestInteractionsStreamImageURIAndBuiltinResultSources(t *testing.T) {
	t.Parallel()

	result := `[{"url":"https://example.com/a","title":"A"}]`
	body := strings.Join([]string{
		`data: {"event_type":"interaction.created","interaction":{"id":"v1_stream","status":"in_progress","model":"gemini-2.5-flash"}}`,
		``,
		`data: {"event_type":"step.start","index":0,"step":{"type":"model_output"}}`,
		``,
		`data: {"event_type":"step.delta","index":0,"delta":{"type":"image","uri":"https://example.com/out.png","mime_type":"image/png"}}`,
		``,
		`data: {"event_type":"step.stop","index":0}`,
		``,
		`data: {"event_type":"step.start","index":1,"step":{"type":"google_search_result","call_id":"search-1","result":` + result + `}}`,
		``,
		`data: {"event_type":"step.stop","index":1}`,
		``,
		`data: {"event_type":"interaction.completed","interaction":{"id":"v1_stream","status":"completed"}}`,
		``,
	}, "\n")
	stream := newInteractionsEventStream(context.Background(), New(Config{APIKey: "test-key"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	defer func() { _ = stream.Close() }()

	var sawURLFile bool
	var sawSource bool
	for {
		chunk, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next: %v", err)
		}
		if chunk.Type == provider.ChunkTypeFile && chunk.GeneratedFileContent != nil && chunk.GeneratedFileContent.URL == "https://example.com/out.png" {
			sawURLFile = true
		}
		if chunk.Type == provider.ChunkTypeSource && chunk.SourceContent != nil && chunk.SourceContent.URL == "https://example.com/a" {
			sawSource = true
		}
	}
	if !sawURLFile {
		t.Fatal("expected streamed image URI file chunk")
	}
	if !sawSource {
		t.Fatal("expected streamed builtin tool result source")
	}
}

// TestInteractionsStreamProcessingAndVideoSteps ports 18ad19c/dc1eb8d to the
// streaming path: a top-level `processing_call`/`processing_result` step
// emits a `custom` chunk carrying `google.processing_call`/
// `google.processing_result` on step.stop, and a `video` step (delivered via
// step.start + step.delta, matching the image pattern) emits a `file` chunk.
func TestInteractionsStreamProcessingAndVideoSteps(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"event_type":"interaction.created","interaction":{"id":"v1_stream","status":"in_progress","model":"gemini-2.5-flash"}}`,
		``,
		`data: {"event_type":"step.start","index":0,"step":{"type":"processing_call","id":"proc-1","signature":"sig-a"}}`,
		``,
		`data: {"event_type":"step.stop","index":0}`,
		``,
		`data: {"event_type":"step.start","index":1,"step":{"type":"processing_result","call_id":"proc-1","signature":"sig-b"}}`,
		``,
		`data: {"event_type":"step.stop","index":1}`,
		``,
		`data: {"event_type":"step.start","index":2,"step":{"type":"video"}}`,
		``,
		`data: {"event_type":"step.delta","index":2,"delta":{"type":"video","uri":"gs://bucket/out.mp4","mime_type":"video/mp4"}}`,
		``,
		`data: {"event_type":"step.stop","index":2}`,
		``,
		`data: {"event_type":"interaction.completed","interaction":{"id":"v1_stream","status":"completed"}}`,
		``,
	}, "\n")
	stream := newInteractionsEventStream(context.Background(), New(Config{APIKey: "test-key"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	defer func() { _ = stream.Close() }()

	var customChunks []*provider.StreamChunk
	var videoChunk *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next: %v", err)
		}
		if chunk.Type == provider.ChunkTypeCustom {
			customChunks = append(customChunks, chunk)
		}
		if chunk.Type == provider.ChunkTypeFile && chunk.GeneratedFileContent != nil && chunk.GeneratedFileContent.MediaType == "video/mp4" {
			videoChunk = chunk
		}
	}
	if len(customChunks) != 2 {
		t.Fatalf("custom chunks = %d, want 2 (processing_call, processing_result)", len(customChunks))
	}
	if customChunks[0].CustomContent == nil || customChunks[0].CustomContent.Kind != "google.processing_call" {
		t.Fatalf("customChunks[0] = %#v", customChunks[0].CustomContent)
	}
	if customChunks[1].CustomContent == nil || customChunks[1].CustomContent.Kind != "google.processing_result" {
		t.Fatalf("customChunks[1] = %#v", customChunks[1].CustomContent)
	}
	if videoChunk == nil || videoChunk.GeneratedFileContent.URL != "gs://bucket/out.mp4" {
		t.Fatalf("videoChunk = %#v", videoChunk)
	}
}

func TestInteractionsStreamErrorEmitsErrorAndFinish(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"event_type":"interaction.created","interaction":{"id":"v1_stream","status":"in_progress","model":"gemini-2.5-flash"}}`,
		``,
		`data: {"event_type":"error","error":{"code":"bad","message":"boom"}}`,
		``,
	}, "\n")
	stream := newInteractionsEventStream(context.Background(), New(Config{APIKey: "test-key"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	defer func() { _ = stream.Close() }()

	var sawError bool
	var finish *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next: %v", err)
		}
		switch chunk.Type {
		case provider.ChunkTypeError:
			sawError = chunk.Text == "boom"
		case provider.ChunkTypeFinish:
			finish = chunk
		}
	}
	if !sawError {
		t.Fatal("expected error chunk")
	}
	if finish == nil || finish.FinishReason != types.FinishReasonError {
		t.Fatalf("finish = %#v", finish)
	}
	var metadata map[string]map[string]interface{}
	if err := json.Unmarshal(finish.ProviderMetadata, &metadata); err != nil {
		t.Fatalf("provider metadata: %v", err)
	}
	if metadata["google"]["usageMetadata"] != nil {
		t.Fatalf("finish provider metadata should not include usageMetadata: %#v", metadata)
	}
}

func TestInteractionsStreamFinishMetadataAlwaysGoogleObject(t *testing.T) {
	t.Parallel()

	body := ""
	stream := newInteractionsEventStream(context.Background(), New(Config{APIKey: "test-key"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	defer func() { _ = stream.Close() }()

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("stream.Next: %v", err)
	}
	if chunk.Type != provider.ChunkTypeFinish {
		t.Fatalf("chunk type = %s", chunk.Type)
	}
	if string(chunk.ProviderMetadata) != `{"google":{}}` {
		t.Fatalf("provider metadata = %s", chunk.ProviderMetadata)
	}
}

// TestInteractionsConvertPromptEmitsFlatTopLevelSteps ports the shape
// asserted by TS convert-to-google-interactions-input.test.ts: the request
// body's `input` is always a flat array of discriminated step objects
// (`GoogleInteractionsInput = Array<Step>`), never `{role, content}` turn
// wrappers, and an assistant message's reasoning/tool-call/processing
// parts each become their OWN top-level step rather than nested content
// inside one combined block — only adjacent text/file content coalesces
// into a single `model_output` step.
func TestInteractionsConvertPromptEmitsFlatTopLevelSteps(t *testing.T) {
	t.Parallel()

	model := NewInteractionsLanguageModel(New(Config{APIKey: "test-key"}), ModelGemini25Flash)
	steps, _, warnings, err := model.convertPrompt(types.Prompt{
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "question"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.TextContent{Text: "part one"},
				types.ReasoningContent{Text: "thinking...", Signature: "sig-r"},
				types.TextContent{Text: "part two"},
			}, ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "lookup", Arguments: map[string]interface{}{"q": "x"}}}},
		},
	}, GoogleInteractionsProviderOptions{})
	if err != nil {
		t.Fatalf("convertPrompt() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}

	// Expected flat step sequence: user_input, model_output("part one"),
	// thought, model_output("part two"), function_call. The tool call
	// (only in msg.ToolCalls, not inline in Content) flushes the pending
	// "part two" text before becoming its own step.
	if len(steps) != 5 {
		t.Fatalf("steps len = %d, want 5: %#v", len(steps), steps)
	}
	wantTypes := []string{"user_input", "model_output", "thought", "model_output", "function_call"}
	for i, want := range wantTypes {
		if got := steps[i]["type"]; got != want {
			t.Fatalf("steps[%d].type = %v, want %q (all steps: %#v)", i, got, want, steps)
		}
	}
	firstModelOutput := steps[1]["content"].([]map[string]interface{})
	if len(firstModelOutput) != 1 || firstModelOutput[0]["text"] != "part one" {
		t.Fatalf("steps[1].content = %#v", firstModelOutput)
	}
	if steps[2]["signature"] != "sig-r" {
		t.Fatalf("steps[2] (thought) = %#v", steps[2])
	}
	secondModelOutput := steps[3]["content"].([]map[string]interface{})
	if len(secondModelOutput) != 1 || secondModelOutput[0]["text"] != "part two" {
		t.Fatalf("steps[3].content = %#v", secondModelOutput)
	}
	if steps[4]["id"] != "call-1" || steps[4]["name"] != "lookup" {
		t.Fatalf("steps[4] (function_call) = %#v", steps[4])
	}
}

func writeSSE(w http.ResponseWriter, data string) {
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
