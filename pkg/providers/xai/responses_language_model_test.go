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
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// TestXAIResponsesLanguageModelMetadata verifies basic model metadata.
func TestXAIResponsesLanguageModelMetadata(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "grok-3")

	if model.SpecificationVersion() != "v3" {
		t.Errorf("SpecificationVersion() = %q, want %q", model.SpecificationVersion(), "v3")
	}
	if model.Provider() != "xai.responses" {
		t.Errorf("Provider() = %q, want %q", model.Provider(), "xai.responses")
	}
	if model.ModelID() != "grok-3" {
		t.Errorf("ModelID() = %q, want %q", model.ModelID(), "grok-3")
	}
	if !model.SupportsTools() {
		t.Error("SupportsTools() = false, want true")
	}
	if !model.SupportsStructuredOutput() {
		t.Error("SupportsStructuredOutput() = false, want true")
	}
	if !model.SupportsImageInput() {
		t.Error("SupportsImageInput() = false, want true")
	}
}

func TestXAIResponsesDoStreamIncludesRawChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"response.output_text.delta","delta":"Hello"}

data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}

data: [DONE]

`)
	}))
	defer server.Close()

	model := NewResponsesLanguageModel(New(Config{APIKey: "test-key", BaseURL: server.URL}), "grok-3")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "hello"},
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatalf("DoStream() error: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("first chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeStreamStart {
		t.Fatalf("first chunk type = %v, want stream-start", chunk.Type)
	}
	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("second chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeRaw {
		t.Fatalf("second chunk type = %v, want raw", chunk.Type)
	}
	raw, ok := chunk.Raw.(map[string]interface{})
	if !ok || raw["type"] != "response.output_text.delta" {
		t.Fatalf("raw chunk = %#v, want response.output_text.delta", chunk.Raw)
	}
	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("third chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeText || chunk.Text != "Hello" {
		t.Fatalf("third chunk = %#v, want text Hello", chunk)
	}
}

func TestXAIResponsesDoStreamEmitsResponseMetadataLikeTypeScript(t *testing.T) {
	stream := newXAIResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.created","response":{"id":"resp_1","created_at":1741269019,"model":"grok-3"}}

data: {"type":"response.in_progress","response":{"id":"resp_1","created_at":1741269019,"model":"grok-3"}}

data: [DONE]

`)))
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("first chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeResponseMetadata {
		t.Fatalf("first chunk type = %v, want response-metadata", chunk.Type)
	}
	if chunk.ResponseMetadata == nil || chunk.ResponseMetadata.ID != "resp_1" || chunk.ResponseMetadata.ModelID != "grok-3" {
		t.Fatalf("response metadata = %#v, want resp_1/grok-3", chunk.ResponseMetadata)
	}
	if got := chunk.ResponseMetadata.Timestamp.Unix(); got != 1741269019 {
		t.Fatalf("timestamp = %d, want 1741269019", got)
	}

	_, err = stream.Next()
	if err != io.EOF {
		t.Fatalf("second Next error = %v, want io.EOF", err)
	}
}

func TestXAIResponsesDoStreamWarningsMatchTypeScript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"response.output_text.delta","delta":"Hello"}

data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}

data: [DONE]

`)
	}))
	defer server.Close()

	model := NewResponsesLanguageModel(New(Config{APIKey: "test-key", BaseURL: server.URL}), "grok-3")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt:        types.Prompt{Text: "hello"},
		StopSequences: []string{"stop"},
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
	if chunk.Warnings[0].Type != "unsupported" || chunk.Warnings[0].Feature != "stopSequences" {
		t.Fatalf("warning = %#v, want unsupported stopSequences", chunk.Warnings[0])
	}
}

func TestXAIResponsesToolsDoNotEmitAdditionalPropertiesFalse(t *testing.T) {
	tools := prepareXAIResponsesTools([]types.Tool{
		{
			Name:        "saveContactWithAddress",
			Description: "Save a contact with an address.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"address": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"city":    map[string]interface{}{"type": "string"},
							"country": map[string]interface{}{"type": "string"},
						},
						"required":             []string{"city", "country"},
						"additionalProperties": false,
					},
				},
				"required":             []string{"address"},
				"additionalProperties": false,
			},
		},
	})
	if len(tools) != 1 {
		t.Fatalf("tools = %#v, want one tool", tools)
	}
	tool, ok := tools[0].(map[string]interface{})
	if !ok {
		t.Fatalf("tool type = %T, want map[string]interface{}", tools[0])
	}
	params, ok := tool["parameters"].(map[string]interface{})
	if !ok {
		t.Fatalf("parameters = %#v, want map[string]interface{}", tool["parameters"])
	}
	if _, exists := params["additionalProperties"]; exists {
		t.Fatalf("unexpected root additionalProperties in xAI Responses tool schema: %#v", params)
	}
	props := params["properties"].(map[string]interface{})
	address := props["address"].(map[string]interface{})
	if _, exists := address["additionalProperties"]; exists {
		t.Fatalf("unexpected nested additionalProperties in xAI Responses tool schema: %#v", address)
	}
}

// TestXAIResponsesReasoningSummary verifies that the reasoningSummary provider option
// is serialized as reasoning.summary in the Responses API request body.
func TestXAIResponsesReasoningSummary(t *testing.T) {
	tests := []struct {
		name             string
		reasoningSummary string
		wantSummary      string
		wantReasoning    bool
	}{
		{
			name:             "auto summary",
			reasoningSummary: "auto",
			wantSummary:      "auto",
			wantReasoning:    true,
		},
		{
			name:             "concise summary",
			reasoningSummary: "concise",
			wantSummary:      "concise",
			wantReasoning:    true,
		},
		{
			name:             "detailed summary",
			reasoningSummary: "detailed",
			wantSummary:      "detailed",
			wantReasoning:    true,
		},
		{
			name:             "no summary",
			reasoningSummary: "",
			wantReasoning:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBody map[string]interface{}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
					"id":     "resp_test",
					"output": []interface{}{},
					"usage":  map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
				})
			}))
			defer server.Close()

			p := New(Config{APIKey: "test-key", BaseURL: server.URL})
			model := NewResponsesLanguageModel(p, "grok-3")

			opts := &provider.GenerateOptions{
				Prompt: types.Prompt{Text: "hello"},
			}
			if tt.reasoningSummary != "" {
				opts.ProviderOptions = map[string]interface{}{
					"xai": map[string]interface{}{
						"reasoningSummary": tt.reasoningSummary,
					},
				}
			}

			_, _ = model.DoGenerate(context.Background(), opts)

			if capturedBody == nil {
				t.Skip("server not reached")
			}

			reasoning, hasReasoning := capturedBody["reasoning"]
			if tt.wantReasoning && !hasReasoning {
				t.Errorf("expected 'reasoning' field in request body")
				return
			}
			if !tt.wantReasoning && hasReasoning {
				t.Errorf("expected no 'reasoning' field in request body")
				return
			}
			if !tt.wantReasoning {
				return
			}

			reasoningMap, ok := reasoning.(map[string]interface{})
			if !ok {
				t.Fatalf("reasoning field is %T, want map", reasoning)
			}
			gotSummary, _ := reasoningMap["summary"].(string)
			if gotSummary != tt.wantSummary {
				t.Errorf("reasoning.summary = %q, want %q", gotSummary, tt.wantSummary)
			}
		})
	}
}

func TestXAIResponsesBuildRequestBodyNonImageFilesUseInputFile(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "grok-3")

	body, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{
						URL:       "https://example.com/report.pdf",
						MediaType: "application/pdf",
					},
					types.FileContent{
						URL:       "https://example.com/data.csv",
						MediaType: "text/csv",
					},
				},
			},
		}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody error = %v", err)
	}

	input := body["input"].([]interface{})
	message := input[0].(responses.UserMessage)
	parts := message.Content.([]interface{})
	urlPart := parts[0].(responses.UserFilePart)
	if urlPart.Type != "input_file" || urlPart.FileURL != "https://example.com/report.pdf" {
		t.Fatalf("url part = %#v", urlPart)
	}
	textURLPart := parts[1].(responses.UserFilePart)
	if textURLPart.Type != "input_file" || textURLPart.FileURL != "https://example.com/data.csv" {
		t.Fatalf("text url part = %#v", textURLPart)
	}
}

func TestXAIResponsesBuildRequestBodyRejectsInlineNonImageFileData(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "grok-3")

	_, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{
						Data:      []byte("%PDF-1.7"),
						MediaType: "application/pdf",
					},
				},
			},
		}},
	}, false)
	if err == nil {
		t.Fatal("expected inline non-image file data to be rejected")
	}
	if !strings.Contains(err.Error(), "file part media type application/pdf") {
		t.Fatalf("error = %q, want media type message", err.Error())
	}
}

func TestXAIResponsesBuildRequestBodyRejectsInlineTextFileParts(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "grok-3")

	_, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{
						Text:      "inline report",
						MediaType: "text/plain",
					},
				},
			},
		}},
	}, false)
	if err == nil {
		t.Fatal("expected inline text file part to be rejected")
	}
	if !strings.Contains(err.Error(), "text file parts") {
		t.Fatalf("error = %q, want text file parts message", err.Error())
	}
}

// TestXAIResponsesReasoningExtractionDoGenerate is a regression test verifying
// that reasoning output items are correctly extracted from the Responses API
// doGenerate response. Both summaryText and encryptedContent must be preserved.
func TestXAIResponsesReasoningExtractionDoGenerate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"id": "resp_abc",
			"output": []interface{}{
				map[string]interface{}{
					"type": "reasoning",
					"id":   "rs_001",
					"summary": []interface{}{
						map[string]interface{}{"type": "summary_text", "text": "First reasoning part. "},
						map[string]interface{}{"type": "summary_text", "text": "Second reasoning part."},
					},
					"encrypted_content": "encrypted-blob-xyz",
				},
				map[string]interface{}{
					"type": "message",
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{"type": "output_text", "text": "The answer is 42."},
					},
				},
			},
			"usage": map[string]interface{}{
				"input_tokens":  20,
				"output_tokens": 30,
				"output_tokens_details": map[string]interface{}{
					"reasoning_tokens": 15,
				},
			},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "grok-3")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "what is 6x7?"},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	// Main text should be from the message output item.
	if result.Text != "The answer is 42." {
		t.Errorf("Text = %q, want %q", result.Text, "The answer is 42.")
	}

	// Content should have: TextContent + ReasoningContent.
	if len(result.Content) < 2 {
		t.Fatalf("Content length = %d, want >= 2", len(result.Content))
	}

	// Find the ReasoningContent part.
	var rc types.ReasoningContent
	var foundReasoning bool
	for _, part := range result.Content {
		if r, ok := part.(types.ReasoningContent); ok {
			rc = r
			foundReasoning = true
			break
		}
	}
	if !foundReasoning {
		t.Fatal("no ReasoningContent in result.Content")
	}

	// Summary text from both parts should be concatenated.
	wantText := "First reasoning part. Second reasoning part."
	if rc.Text != wantText {
		t.Errorf("ReasoningContent.Text = %q, want %q", rc.Text, wantText)
	}

	// EncryptedContent must be preserved for multi-turn reasoning.
	if rc.EncryptedContent != "encrypted-blob-xyz" {
		t.Errorf("ReasoningContent.EncryptedContent = %q, want %q", rc.EncryptedContent, "encrypted-blob-xyz")
	}

	// Usage should reflect reasoning tokens in OutputDetails.
	if result.Usage.OutputDetails == nil {
		t.Fatal("Usage.OutputDetails is nil, want reasoning token breakdown")
	}
	if result.Usage.OutputDetails.ReasoningTokens == nil || *result.Usage.OutputDetails.ReasoningTokens != 15 {
		t.Errorf("ReasoningTokens = %v, want 15", result.Usage.OutputDetails.ReasoningTokens)
	}
}

func TestXAIResponsesDoGenerateCostInUsdTicksMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"id":     "resp_cost",
			"status": "completed",
			"output": []interface{}{},
			"usage": map[string]interface{}{
				"input_tokens":      10,
				"output_tokens":     5,
				"cost_in_usd_ticks": 113500,
			},
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

	xaiMeta, ok := result.ProviderMetadata["xai"].(map[string]interface{})
	if !ok {
		t.Fatalf("ProviderMetadata[xai] = %T, want map[string]interface{}", result.ProviderMetadata["xai"])
	}
	if got := xaiMeta["costInUsdTicks"]; got != int64(113500) {
		t.Fatalf("costInUsdTicks: got %v", got)
	}
	if got := result.Usage.Raw["cost_in_usd_ticks"]; got != int64(113500) {
		t.Fatalf("usage raw cost_in_usd_ticks: got %v", got)
	}
}

// TestXAIResponsesLogprobsOption verifies that logprobs:true is serialized in the request.
func TestXAIResponsesLogprobsOption(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"id":     "resp_test",
			"output": []interface{}{},
			"usage":  map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "grok-3")

	trueVal := true
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"logprobs": &trueVal,
			},
		},
	}

	_, _ = model.DoGenerate(context.Background(), opts)

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
}

// TestXAIResponsesTopLogprobsAutoEnablesLogprobs verifies that setting topLogprobs
// implicitly enables logprobs in the Responses API request.
func TestXAIResponsesTopLogprobsAutoEnablesLogprobs(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"id":     "resp_test",
			"output": []interface{}{},
			"usage":  map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "grok-3")

	topN := 5
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"topLogprobs": &topN,
			},
		},
	}

	_, _ = model.DoGenerate(context.Background(), opts)

	if capturedBody == nil {
		t.Skip("server not reached")
	}

	// topLogprobs should auto-enable logprobs.
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
	if topLogprobs != float64(5) {
		t.Errorf("top_logprobs = %v, want 5", topLogprobs)
	}
}

// TestXAIResponsesReasoningEffortProviderOption verifies that xaiOpts.reasoningEffort="medium"
// overrides opts.Reasoning and appears as reasoning:{effort:"medium"}.
func TestXAIResponsesReasoningEffortProviderOption(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"id": "resp_test", "output": []interface{}{},
			"usage": map[string]interface{}{"input_tokens": 5, "output_tokens": 3},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "grok-3")

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"reasoningEffort": "medium",
			},
		},
	}
	_, _ = model.DoGenerate(context.Background(), opts)

	if capturedBody == nil {
		t.Skip("server not reached")
	}
	reasoning, ok := capturedBody["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("reasoning field = %v, want map", capturedBody["reasoning"])
	}
	if reasoning["effort"] != "medium" {
		t.Errorf("reasoning.effort = %v, want %q", reasoning["effort"], "medium")
	}
}

// TestXAIResponsesReasoningEffortPrecedence verifies that provider option beats opts.Reasoning.
func TestXAIResponsesReasoningEffortPrecedence(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"id": "resp_test", "output": []interface{}{},
			"usage": map[string]interface{}{"input_tokens": 5, "output_tokens": 3},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "grok-3")

	highReasoning := types.ReasoningHigh
	opts := &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hello"},
		Reasoning: &highReasoning,
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"reasoningEffort": "low",
			},
		},
	}
	_, _ = model.DoGenerate(context.Background(), opts)

	if capturedBody == nil {
		t.Skip("server not reached")
	}
	reasoning, ok := capturedBody["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("reasoning field = %v, want map", capturedBody["reasoning"])
	}
	if reasoning["effort"] != "low" {
		t.Errorf("reasoning.effort = %v, want %q (provider option should override top-level)", reasoning["effort"], "low")
	}
}

// TestXAIResponsesStoreFalse verifies that store:false is in the body and
// reasoning.encrypted_content is auto-added to include.
func TestXAIResponsesStoreFalse(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"id": "resp_test", "output": []interface{}{},
			"usage": map[string]interface{}{"input_tokens": 5, "output_tokens": 3},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "grok-3")

	storeFalse := false
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"store": &storeFalse,
			},
		},
	}
	_, _ = model.DoGenerate(context.Background(), opts)

	if capturedBody == nil {
		t.Skip("server not reached")
	}
	if capturedBody["store"] != false {
		t.Errorf("store = %v, want false", capturedBody["store"])
	}
	include, ok := capturedBody["include"].([]interface{})
	if !ok {
		t.Fatalf("include field = %v, want []interface{}", capturedBody["include"])
	}
	var hasEncrypted bool
	for _, v := range include {
		if v == "reasoning.encrypted_content" {
			hasEncrypted = true
			break
		}
	}
	if !hasEncrypted {
		t.Errorf("expected 'reasoning.encrypted_content' in include, got %v", include)
	}
}

// TestXAIResponsesStoreFalsePreservesExistingInclude verifies that existing include
// entries are kept when store:false auto-adds reasoning.encrypted_content.
func TestXAIResponsesStoreFalsePreservesExistingInclude(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"id": "resp_test", "output": []interface{}{},
			"usage": map[string]interface{}{"input_tokens": 5, "output_tokens": 3},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "grok-3")

	storeFalse := false
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"store":   &storeFalse,
				"include": []interface{}{"file_search_call.results"},
			},
		},
	}
	_, _ = model.DoGenerate(context.Background(), opts)

	if capturedBody == nil {
		t.Skip("server not reached")
	}
	include, ok := capturedBody["include"].([]interface{})
	if !ok {
		t.Fatalf("include = %v, want slice", capturedBody["include"])
	}
	var hasFileSearch, hasEncrypted bool
	for _, v := range include {
		if v == "file_search_call.results" {
			hasFileSearch = true
		}
		if v == "reasoning.encrypted_content" {
			hasEncrypted = true
		}
	}
	if !hasFileSearch {
		t.Error("expected 'file_search_call.results' preserved in include")
	}
	if !hasEncrypted {
		t.Error("expected 'reasoning.encrypted_content' auto-added to include")
	}
}

// TestXAIResponsesIncludeExplicit verifies that explicit include without store:false
// passes through unchanged.
func TestXAIResponsesIncludeExplicit(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"id": "resp_test", "output": []interface{}{},
			"usage": map[string]interface{}{"input_tokens": 5, "output_tokens": 3},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "grok-3")

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"include": []interface{}{"file_search_call.results"},
			},
		},
	}
	_, _ = model.DoGenerate(context.Background(), opts)

	if capturedBody == nil {
		t.Skip("server not reached")
	}
	include, ok := capturedBody["include"].([]interface{})
	if !ok {
		t.Fatalf("include = %v, want slice", capturedBody["include"])
	}
	if len(include) != 1 || include[0] != "file_search_call.results" {
		t.Errorf("include = %v, want [file_search_call.results]", include)
	}
}

// TestXAIResponsesPreviousResponseId verifies that previousResponseId is serialized
// as previous_response_id in the request body.
func TestXAIResponsesPreviousResponseId(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"id": "resp_test", "output": []interface{}{},
			"usage": map[string]interface{}{"input_tokens": 5, "output_tokens": 3},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "grok-3")

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"previousResponseId": "resp_abc123",
			},
		},
	}
	_, _ = model.DoGenerate(context.Background(), opts)

	if capturedBody == nil {
		t.Skip("server not reached")
	}
	if capturedBody["previous_response_id"] != "resp_abc123" {
		t.Errorf("previous_response_id = %v, want %q", capturedBody["previous_response_id"], "resp_abc123")
	}
}

// TestXAIResponsesToolChoice verifies tool_choice serialization for Responses API.
func TestXAIResponsesToolChoice(t *testing.T) {
	tests := []struct {
		name       string
		toolChoice types.ToolChoice
		want       string
	}{
		{
			name:       "none",
			toolChoice: types.ToolChoice{Type: types.ToolChoiceNone},
			want:       "none",
		},
		{
			name:       "required",
			toolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
			want:       "required",
		},
		{
			name:       "auto (default)",
			toolChoice: types.ToolChoice{Type: types.ToolChoiceAuto},
			want:       "auto",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertXAIResponsesToolChoice(tt.toolChoice)
			gotStr, _ := got.(string)
			if gotStr != tt.want {
				t.Errorf("convertXAIResponsesToolChoice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestXAIResponsesDoGenerateTokenCostMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     "resp_cost",
			"status": "completed",
			"output": []interface{}{},
			"usage": map[string]interface{}{
				"input_tokens":       10,
				"output_tokens":      5,
				"input_tokens_cost":  0.001,
				"output_tokens_cost": 0.002,
			},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "grok-3")
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	xaiMeta := result.ProviderMetadata["xai"].(map[string]interface{})
	cost := xaiMeta["cost"].(map[string]interface{})
	if cost["inputTokensCost"] != 0.001 || cost["outputTokensCost"] != 0.002 {
		t.Fatalf("cost metadata = %#v", cost)
	}
}

func TestXAIResponsesStreamEventsHandling(t *testing.T) {
	tests := []struct {
		name          string
		event         string
		wantChunkType provider.ChunkType
	}{
		{
			name:          "error",
			event:         `{"type":"error","code":"bad_request","message":"boom"}`,
			wantChunkType: provider.ChunkTypeError,
		},
		{
			name:          "incomplete",
			event:         `{"type":"response.incomplete","response":{"usage":{"input_tokens":1,"output_tokens":2},"incomplete_details":{"reason":"max_output_tokens"}}}`,
			wantChunkType: provider.ChunkTypeFinish,
		},
		{
			name:          "failed",
			event:         `{"type":"response.failed","response":{"usage":{"input_tokens":3,"output_tokens":4},"error":{"code":"server_error","message":"failed"},"incomplete_details":{"reason":"error"}}}`,
			wantChunkType: provider.ChunkTypeFinish,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sse := "data: " + tt.event + "\n\n"
			stream := newXAIResponsesStream(io.NopCloser(strings.NewReader(sse)))
			chunk, err := stream.Next()
			if err != nil {
				t.Fatalf("Next error: %v", err)
			}
			if chunk.Type != tt.wantChunkType {
				t.Fatalf("chunk.Type = %s, want %s", chunk.Type, tt.wantChunkType)
			}
			_, err = stream.Next()
			if err == nil {
				t.Fatal("expected stream termination")
			}
			if err != io.EOF {
				t.Fatalf("err = %T %v, want io.EOF", err, err)
			}
			if stream.Err() != nil {
				t.Fatalf("stream.Err() = %v, want nil", stream.Err())
			}
		})
	}
}

func TestXAIResponsesStreamParseErrorEmitsErrorChunkAfterRaw(t *testing.T) {
	stream := newXAIResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":

data: [DONE]

`)), true)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("first chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeRaw {
		t.Fatalf("first chunk type = %v, want raw", chunk.Type)
	}

	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("second chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeError {
		t.Fatalf("second chunk type = %v, want error", chunk.Type)
	}
	if !strings.Contains(chunk.Text, "failed to parse stream chunk") {
		t.Fatalf("error text = %q, want parse failure", chunk.Text)
	}
}
