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
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("X-Gemini-Service-Tier", "priority")
		_, _ = fmt.Fprint(w, `{
			"id":"v1_test",
			"status":"completed",
			"model":"gemini-2.5-flash",
			"created":"2026-05-04T19:00:00Z",
			"outputs":[
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
	if captured["response_mime_type"] != "application/json" {
		t.Fatalf("response_mime_type = %v", captured["response_mime_type"])
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
	input := captured["input"].([]interface{})
	fileBlock := input[1].(map[string]interface{})
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
}

func TestInteractionsPreviousInteractionCompactsAssistantAndToolResult(t *testing.T) {
	t.Parallel()

	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = fmt.Fprint(w, `{"id":"v1_next","status":"completed","outputs":[{"type":"text","text":"next"}]}`)
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
	turns := captured["input"].([]interface{})
	if len(turns) != 2 {
		t.Fatalf("turn count = %d, body=%#v", len(turns), captured["input"])
	}
	for _, turn := range turns {
		if turn.(map[string]interface{})["role"] == "model" {
			t.Fatalf("assistant turn was not compacted: %#v", turns)
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
		_, _ = fmt.Fprint(w, `{"id":"v1_agent","status":"completed","outputs":[{"type":"text","text":"ok"}]}`)
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
		_, _ = fmt.Fprint(w, `{"id":"v1_img","status":"completed","outputs":[{"type":"text","text":"ok"}]}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.Interactions(InteractionsModelGemini3ProImage)
	if err != nil {
		t.Fatalf("Interactions: %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
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
	generationConfig := captured["generation_config"].(map[string]interface{})
	imageConfig := generationConfig["image_config"].(map[string]interface{})
	if imageConfig["aspect_ratio"] != "16:9" || imageConfig["image_size"] != "2K" {
		t.Fatalf("image_config = %#v", imageConfig)
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
		{Type: "function_call", ID: "call-1", Name: "lookup", Arguments: map[string]interface{}{"q": "x"}, Signature: "sig"},
		{Type: "text", Text: "after"},
		{Type: "google_search_call", ID: "search-1", Arguments: map[string]interface{}{"query": "go"}},
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
	content, warnings, err := model.convertAssistantParts([]types.ContentPart{
		types.TextContent{Text: "before"},
		types.ToolCallContent{
			ToolCallID:       "call-1",
			ToolName:         "lookup",
			Input:            `{"q":"x"}`,
			ProviderMetadata: providerMetaRaw("sig-1", "v1_prev"),
		},
	}, nil, "")
	if err != nil {
		t.Fatalf("convertAssistantParts() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if len(content) != 2 {
		t.Fatalf("content len = %d", len(content))
	}
	call := content[1]
	if call["type"] != "function_call" || call["id"] != "call-1" || call["name"] != "lookup" {
		t.Fatalf("function_call block = %#v", call)
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

func TestInteractionsGenerateEmptyProviderMetadataGoogleObject(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"status":"completed","outputs":[{"type":"text","text":"ok"}]}`)
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
				writeSSE(w, `{"event_type":"interaction.start","interaction":{"id":"v1_stream","status":"in_progress","model":"gemini-2.5-flash"}}`)
				writeSSE(w, `{"event_type":"content.start","index":0,"content":{"type":"text"}}`)
				writeSSE(w, `{"event_type":"content.delta","index":0,"delta":{"type":"text","text":"hel"}}`)
				return
			}
			writeSSE(w, `{"event_type":"content.delta","index":0,"delta":{"type":"text","text":"lo"}}`)
			writeSSE(w, `{"event_type":"content.stop","index":0}`)
			writeSSE(w, `{"event_type":"interaction.complete","interaction":{"id":"v1_stream","status":"completed","usage":{"total_tokens":2},"service_tier":"standard"}}`)
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
	defer stream.Close()

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
		`data: {"event_type":"interaction.start","interaction":{"id":"v1_stream","status":"in_progress","model":"gemini-2.5-flash"}}`,
		``,
		`data: {"event_type":"content.start","index":0,"content":{"type":"function_call","id":"call-1","name":"lookup"}}`,
		``,
		`data: {"event_type":"content.delta","index":0,"delta":{"type":"function_call","id":"call-1","name":"lookup","arguments":{"q":"x"}}}`,
		``,
		`data: {"event_type":"content.stop","index":0}`,
		``,
		`data: {"event_type":"interaction.complete","interaction":{"id":"v1_stream","status":"completed"}}`,
		``,
	}, "\n")
	stream := newInteractionsEventStream(context.Background(), New(Config{APIKey: "test-key"}), io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	defer stream.Close()

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
		`data: {"event_type":"interaction.start","interaction":{"id":"v1_stream","status":"in_progress","model":"gemini-2.5-flash"}}`,
		``,
		`data: {"event_type":"interaction.status_update","status":"incomplete"}`,
		``,
		`data: {"event_type":"content.start","index":0,"content":{"type":"text"}}`,
		``,
		`data: {"event_type":"content.delta","index":0,"delta":{"type":"text","text":"partial"}}`,
		``,
	}, "\n")
	stream := newInteractionsEventStream(context.Background(), New(Config{APIKey: "test-key"}), io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	defer stream.Close()

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
		`data: {"event_type":"interaction.start","interaction":{"id":"v1_stream","status":"in_progress","model":"gemini-2.5-flash"}}`,
		``,
		`data: {"event_type":"content.start","index":0,"content":{"type":"image","uri":"https://example.com/out.png","mime_type":"image/png"}}`,
		``,
		`data: {"event_type":"content.stop","index":0}`,
		``,
		`data: {"event_type":"content.start","index":1,"content":{"type":"google_search_result","call_id":"search-1","result":` + result + `}}`,
		``,
		`data: {"event_type":"content.stop","index":1}`,
		``,
		`data: {"event_type":"interaction.complete","interaction":{"id":"v1_stream","status":"completed"}}`,
		``,
	}, "\n")
	stream := newInteractionsEventStream(context.Background(), New(Config{APIKey: "test-key"}), io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	defer stream.Close()

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

func TestInteractionsStreamErrorEmitsErrorAndFinish(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"event_type":"interaction.start","interaction":{"id":"v1_stream","status":"in_progress","model":"gemini-2.5-flash"}}`,
		``,
		`data: {"event_type":"error","error":{"code":"bad","message":"boom"}}`,
		``,
	}, "\n")
	stream := newInteractionsEventStream(context.Background(), New(Config{APIKey: "test-key"}), io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	defer stream.Close()

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
	stream := newInteractionsEventStream(context.Background(), New(Config{APIKey: "test-key"}), io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	defer stream.Close()

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

func writeSSE(w http.ResponseWriter, data string) {
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
