package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
						Data:      []byte("a,b\n1,2\n"),
						MediaType: "text/csv",
						Filename:  "data.csv",
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
	dataPart := parts[1].(map[string]interface{})
	if dataPart["type"] != "input_file" || dataPart["filename"] != "data.csv" {
		t.Fatalf("data part = %#v", dataPart)
	}
	if _, ok := dataPart["file_data"].(string); !ok {
		t.Fatalf("data part missing file_data: %#v", dataPart)
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
