package anthropic

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// --- output_config.format tests (ANT-T04) ---

// TestOutputConfigFormat verifies that when ResponseFormat is set with a JSON schema,
// the request body uses output_config.format instead of the old output_format field.
func TestOutputConfigFormat(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
	}

	tests := []struct {
		name           string
		responseFormat *provider.ResponseFormat
		wantOutputConfig bool
		wantSchema     bool
	}{
		{
			name: "json with schema uses output_config",
			responseFormat: &provider.ResponseFormat{
				Type:   "json",
				Schema: schema,
			},
			wantOutputConfig: true,
			wantSchema:     true,
		},
		{
			name: "json_schema type uses output_config",
			responseFormat: &provider.ResponseFormat{
				Type:   "json_schema",
				Schema: schema,
			},
			wantOutputConfig: true,
			wantSchema:     true,
		},
		{
			name: "json without schema skips output_config",
			responseFormat: &provider.ResponseFormat{
				Type: "json",
			},
			wantOutputConfig: false,
		},
		{
			name:             "nil response format — no output_config",
			responseFormat:   nil,
			wantOutputConfig: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := New(Config{APIKey: "test-key"})
			model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

			opts := &provider.GenerateOptions{
				Prompt: types.Prompt{Text: "Tell me a joke"},
				ResponseFormat: tt.responseFormat,
			}

			body := model.buildRequestBody(opts, false)

			_, hasOutputConfig := body["output_config"]
			if hasOutputConfig != tt.wantOutputConfig {
				t.Errorf("output_config presence = %v, want %v", hasOutputConfig, tt.wantOutputConfig)
				return
			}

			if tt.wantOutputConfig {
				// Verify old output_format key is NOT present
				if _, hasOldField := body["output_format"]; hasOldField {
					t.Error("body should not contain old 'output_format' key")
				}

				// Verify output_config structure
				outputConfig, ok := body["output_config"].(map[string]interface{})
				if !ok {
					t.Fatal("output_config is not a map")
				}

				format, hasFormat := outputConfig["format"]
				if !hasFormat {
					t.Error("output_config missing 'format' key")
					return
				}

				if tt.wantSchema {
					formatMap, ok := format.(map[string]interface{})
					if !ok {
						t.Fatal("output_config.format is not a map")
					}
					if formatMap["type"] != "json_schema" {
						t.Errorf("output_config.format.type = %v, want json_schema", formatMap["type"])
					}
					if formatMap["schema"] == nil {
						t.Error("output_config.format.schema should not be nil")
					}
				}
			}
		})
	}
}

// TestOutputConfigJSONSerialization verifies the request body serializes correctly to JSON.
func TestOutputConfigJSONSerialization(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"answer": map[string]interface{}{"type": "string"},
		},
	}

	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Answer in JSON"},
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json",
			Schema: schema,
		},
	}

	body := model.buildRequestBody(opts, false)

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Confirm the serialized JSON contains output_config.format
	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"output_config"`) {
		t.Error("serialized JSON missing output_config")
	}
	if !strings.Contains(jsonStr, `"json_schema"`) {
		t.Error("serialized JSON missing json_schema type")
	}
	if strings.Contains(jsonStr, `"output_format"`) {
		t.Error("serialized JSON should not contain old output_format field")
	}
}

// --- Automatic caching tests (ANT-T10) ---

// TestAutomaticCachingBetaHeader verifies the correct beta header is sent when automatic caching is enabled.
func TestAutomaticCachingBetaHeader(t *testing.T) {
	tests := []struct {
		name             string
		automaticCaching bool
		wantHeader       bool
		wantHeaderValue  string
	}{
		{
			name:             "automatic caching enabled",
			automaticCaching: true,
			wantHeader:       true,
			wantHeaderValue:  BetaHeaderPromptCaching,
		},
		{
			name:             "automatic caching disabled",
			automaticCaching: false,
			wantHeader:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := New(Config{APIKey: "test-key"})
			model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
				AutomaticCaching: tt.automaticCaching,
			})

			betaHeader := model.getBetaHeaders()
			hasCachingHeader := strings.Contains(betaHeader, BetaHeaderPromptCaching)

			if hasCachingHeader != tt.wantHeader {
				t.Errorf("caching beta header presence = %v, want %v (header=%q)", hasCachingHeader, tt.wantHeader, betaHeader)
			}
		})
	}
}

// TestAutomaticCachingRequestBody verifies cache_control: {type: "auto"} is added to the body.
func TestAutomaticCachingRequestBody(t *testing.T) {
	tests := []struct {
		name             string
		automaticCaching bool
		wantCacheControl bool
	}{
		{
			name:             "automatic caching enabled adds cache_control",
			automaticCaching: true,
			wantCacheControl: true,
		},
		{
			name:             "automatic caching disabled omits cache_control",
			automaticCaching: false,
			wantCacheControl: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := New(Config{APIKey: "test-key"})
			model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
				AutomaticCaching: tt.automaticCaching,
			})

			opts := &provider.GenerateOptions{
				Prompt: types.Prompt{Text: "Hello"},
			}

			body := model.buildRequestBody(opts, false)

			cacheControl, hasCacheControl := body["cache_control"]
			if hasCacheControl != tt.wantCacheControl {
				t.Errorf("cache_control presence = %v, want %v", hasCacheControl, tt.wantCacheControl)
				return
			}

			if tt.wantCacheControl {
				ccMap, ok := cacheControl.(map[string]string)
				if !ok {
					t.Fatal("cache_control is not map[string]string")
				}
				if ccMap["type"] != "auto" {
					t.Errorf("cache_control.type = %q, want %q", ccMap["type"], "auto")
				}
			}
		})
	}
}

// TestAutomaticCachingWithOtherOptions verifies automatic caching works alongside other features.
func TestAutomaticCachingWithOtherOptions(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeOpus4_6, &ModelOptions{
		AutomaticCaching: true,
		Speed:            SpeedFast,
	})

	// Beta header should contain both automatic caching and fast mode
	betaHeader := model.getBetaHeaders()
	if !strings.Contains(betaHeader, BetaHeaderPromptCaching) {
		t.Errorf("missing prompt caching header, got: %q", betaHeader)
	}
	if !strings.Contains(betaHeader, BetaHeaderFastMode) {
		t.Errorf("missing fast mode header, got: %q", betaHeader)
	}

	// Request body should have cache_control
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Test"},
	}
	body := model.buildRequestBody(opts, false)
	if _, ok := body["cache_control"]; !ok {
		t.Error("cache_control missing from body when automatic caching is enabled")
	}
}

// --- Model ID constant tests (ANT-T14) ---

// TestClaudeSonnet4_6ModelID verifies the new model ID constant is accepted.
func TestClaudeSonnet4_6ModelID(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})

	model, err := prov.LanguageModel(ClaudeSonnet4_6)
	if err != nil {
		t.Fatalf("LanguageModel(%q) returned error: %v", ClaudeSonnet4_6, err)
	}

	if model.ModelID() != ClaudeSonnet4_6 {
		t.Errorf("ModelID() = %q, want %q", model.ModelID(), ClaudeSonnet4_6)
	}

	if model.Provider() != "anthropic" {
		t.Errorf("Provider() = %q, want %q", model.Provider(), "anthropic")
	}
}

// TestModelIDConstants verifies all model ID constants are non-empty strings.
func TestModelIDConstants(t *testing.T) {
	constants := map[string]string{
		"ClaudeOpus4_6":            ClaudeOpus4_6,
		"ClaudeSonnet4_6":          ClaudeSonnet4_6,
		"ClaudeOpus4_5_20251101":   ClaudeOpus4_5_20251101,
		"ClaudeOpus4_5":            ClaudeOpus4_5,
		"ClaudeOpus4_20250514":     ClaudeOpus4_20250514,
		"ClaudeSonnet4_5_20250929": ClaudeSonnet4_5_20250929,
		"ClaudeSonnet4_5":          ClaudeSonnet4_5,
		"ClaudeSonnet4_20250514":   ClaudeSonnet4_20250514,
		"ClaudeHaiku4_5_20251001":  ClaudeHaiku4_5_20251001,
		"ClaudeHaiku4_5":           ClaudeHaiku4_5,
	}

	for name, id := range constants {
		if id == "" {
			t.Errorf("constant %s is empty", name)
		}
	}
}

// TestSupportsStructuredOutput verifies SupportsStructuredOutput returns true for models
// that support output_config.format, matching the TS SDK getModelCapabilities() logic.
func TestSupportsStructuredOutput(t *testing.T) {
	tests := []struct {
		modelID string
		want    bool
	}{
		// 4.6 models — true
		{ClaudeOpus4_6, true},
		{ClaudeSonnet4_6, true},
		// 4.5 models — true
		{ClaudeOpus4_5, true},
		{ClaudeOpus4_5_20251101, true},
		{ClaudeSonnet4_5, true},
		{ClaudeSonnet4_5_20250929, true},
		{ClaudeHaiku4_5, true},
		{ClaudeHaiku4_5_20251001, true},
		// Older 4.x models (e.g. claude-sonnet-4-20250514) — false
		{ClaudeSonnet4_20250514, false},
		{ClaudeOpus4_20250514, false},
		// claude-3.7, claude-3.5, claude-3 — false
		{Claude3_7Sonnet_20250219, false},
		{Claude3_5Sonnet_20241022, false},
		{Claude3_5Haiku_20241022, false},
		{Claude3Opus_20240229, false},
		{Claude3Haiku_20240307, false},
	}

	prov := New(Config{APIKey: "test-key"})
	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			model := NewLanguageModel(prov, tt.modelID, nil)
			got := model.SupportsStructuredOutput()
			if got != tt.want {
				t.Errorf("SupportsStructuredOutput() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Compaction delta null content tests (ANT-T16) ---

// TestCompactionDeltaNullContent verifies that a null content in compaction_delta
// does not cause a parse error or panic in the stream parser.
func TestCompactionDeltaNullContent(t *testing.T) {
	// Simulate an SSE stream with a compaction_delta event where content is null
	sseData := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"compaction_delta\",\"content\":null}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	// First chunk: compaction_delta with null content → should be skipped (Next() recurses)
	// Second chunk: text_delta → should return text
	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() returned error on compaction_delta with null content: %v", err)
	}
	if chunk == nil {
		t.Fatal("Next() returned nil chunk, expected text chunk")
	}
	if chunk.Type != provider.ChunkTypeText {
		t.Errorf("chunk.Type = %v, want ChunkTypeText", chunk.Type)
	}
	if chunk.Text != "Hello" {
		t.Errorf("chunk.Text = %q, want %q", chunk.Text, "Hello")
	}

	// Next call should return EOF
	_, err = stream.Next()
	if err != io.EOF {
		t.Errorf("expected io.EOF after message_stop, got: %v", err)
	}
}

// TestCompactionDeltaWithContent verifies compaction_delta with non-null content is emitted
// as a text-delta chunk (matching TS SDK behavior).
func TestCompactionDeltaWithContent(t *testing.T) {
	sseData := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"compaction_delta\",\"content\":\"summary text\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	// Non-null compaction content → emitted as text chunk
	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeText {
		t.Errorf("chunk.Type = %v, want ChunkTypeText", chunk.Type)
	}
	if chunk.Text != "summary text" {
		t.Errorf("chunk.Text = %q, want %q", chunk.Text, "summary text")
	}

	// Next → EOF
	_, err = stream.Next()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got: %v", err)
	}
}

// --- compaction_delta content emission tests ---

// TestCompactionDeltaEmitsContent verifies non-null compaction_delta content is emitted as text.
func TestCompactionDeltaEmitsContent(t *testing.T) {
	sseData := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"compaction_delta\",\"content\":\"summary of prior conversation\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeText {
		t.Errorf("chunk.Type = %v, want ChunkTypeText", chunk.Type)
	}
	if chunk.Text != "summary of prior conversation" {
		t.Errorf("chunk.Text = %q, want %q", chunk.Text, "summary of prior conversation")
	}
}

// --- Effort option tests ---

// TestEffortBetaHeader verifies the effort-2025-11-24 beta header is added when Effort is set.
func TestEffortBetaHeader(t *testing.T) {
	tests := []struct {
		name       string
		effort     Effort
		wantHeader bool
	}{
		{"effort high adds header", EffortHigh, true},
		{"effort low adds header", EffortLow, true},
		{"effort max adds header", EffortMax, true},
		{"no effort no header", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := New(Config{APIKey: "test-key"})
			model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
				Effort: tt.effort,
			})
			h := model.getBetaHeaders()
			has := strings.Contains(h, BetaHeaderEffort)
			if has != tt.wantHeader {
				t.Errorf("effort header presence = %v, want %v (headers=%q)", has, tt.wantHeader, h)
			}
		})
	}
}

// TestEffortInOutputConfig verifies the effort value is included in output_config.
func TestEffortInOutputConfig(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{Effort: EffortMedium})

	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "test"},
	}, false)

	oc, ok := body["output_config"].(map[string]interface{})
	if !ok {
		t.Fatal("output_config not present or wrong type")
	}
	if oc["effort"] != "medium" {
		t.Errorf("output_config.effort = %v, want %q", oc["effort"], "medium")
	}
}

// TestEffortAndFormatCombined verifies effort and ResponseFormat both appear in output_config.
func TestEffortAndFormatCombined(t *testing.T) {
	schema := map[string]interface{}{"type": "object"}
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{Effort: EffortHigh})

	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "test"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: schema},
	}, false)

	oc, ok := body["output_config"].(map[string]interface{})
	if !ok {
		t.Fatal("output_config not present or wrong type")
	}
	if oc["effort"] != "high" {
		t.Errorf("output_config.effort = %v, want %q", oc["effort"], "high")
	}
	if oc["format"] == nil {
		t.Error("output_config.format should be present alongside effort")
	}
}

// --- CacheControl option tests ---

// TestCacheControlEphemeral verifies CacheControl adds ephemeral cache_control to the body.
func TestCacheControlEphemeral(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
		CacheControl: &CacheControlOption{Type: "ephemeral", TTL: "5m"},
	})

	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "test"},
	}, false)

	cc, ok := body["cache_control"].(*CacheControlOption)
	if !ok {
		t.Fatal("cache_control is not *CacheControlOption")
	}
	if cc.Type != "ephemeral" {
		t.Errorf("cache_control.type = %q, want %q", cc.Type, "ephemeral")
	}
	if cc.TTL != "5m" {
		t.Errorf("cache_control.ttl = %q, want %q", cc.TTL, "5m")
	}
}

// TestCacheControlTakesPrecedenceOverAutomatic verifies CacheControl wins over AutomaticCaching.
func TestCacheControlTakesPrecedenceOverAutomatic(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
		AutomaticCaching: true,
		CacheControl:     &CacheControlOption{Type: "ephemeral"},
	})

	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "test"},
	}, false)

	cc, ok := body["cache_control"].(*CacheControlOption)
	if !ok {
		t.Fatalf("cache_control should be *CacheControlOption when both set, got %T", body["cache_control"])
	}
	if cc.Type != "ephemeral" {
		t.Errorf("CacheControl should take precedence: got type=%q", cc.Type)
	}
}

// --- Fine-grained tool streaming header tests ---

// TestFineGrainedToolStreamingHeader verifies the header is added on streaming but not generate.
func TestFineGrainedToolStreamingHeader(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "test"}}

	// Streaming: header should be present by default
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)
	streamHeader := model.combineBetaHeaders(opts, true)
	if !strings.Contains(streamHeader, BetaHeaderFineGrainedToolStreaming) {
		t.Errorf("streaming should include fine-grained header, got: %q", streamHeader)
	}

	// Non-streaming: header should NOT be present
	generateHeader := model.combineBetaHeaders(opts, false)
	if strings.Contains(generateHeader, BetaHeaderFineGrainedToolStreaming) {
		t.Errorf("non-streaming should NOT include fine-grained header, got: %q", generateHeader)
	}
}

// TestFineGrainedToolStreamingDisabled verifies the header is suppressed when ToolStreaming=false.
func TestFineGrainedToolStreamingDisabled(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	disabled := false
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
		ToolStreaming: &disabled,
	})

	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "test"}}
	streamHeader := model.combineBetaHeaders(opts, true)
	if strings.Contains(streamHeader, BetaHeaderFineGrainedToolStreaming) {
		t.Errorf("ToolStreaming=false should suppress fine-grained header, got: %q", streamHeader)
	}
}

// --- Thinking strips temperature/topP/topK tests ---

// TestThinkingStripsTemperatureParams verifies that temperature, top_k, and top_p are NOT
// sent when thinking is enabled (Anthropic API rejects them in that case).
func TestThinkingStripsTemperatureParams(t *testing.T) {
	temp := 0.7
	topP := 0.9
	topK := 40

	prov := New(Config{APIKey: "test-key"})

	tests := []struct {
		name      string
		thinking  *ThinkingConfig
		wantStrip bool
	}{
		{
			name:      "adaptive thinking strips params",
			thinking:  &ThinkingConfig{Type: ThinkingTypeAdaptive},
			wantStrip: true,
		},
		{
			name:      "enabled thinking strips params",
			thinking:  &ThinkingConfig{Type: ThinkingTypeEnabled},
			wantStrip: true,
		},
		{
			name:      "disabled thinking keeps params",
			thinking:  &ThinkingConfig{Type: ThinkingTypeDisabled},
			wantStrip: false,
		},
		{
			name:      "no thinking config keeps params",
			thinking:  nil,
			wantStrip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{Thinking: tt.thinking})
			opts := &provider.GenerateOptions{
				Prompt:      types.Prompt{Text: "test"},
				Temperature: &temp,
				TopP:        &topP,
				TopK:        &topK,
			}
			body := model.buildRequestBody(opts, false)

			_, hasTemp := body["temperature"]
			_, hasTopP := body["top_p"]
			_, hasTopK := body["top_k"]

			if tt.wantStrip {
				if hasTemp {
					t.Error("temperature should be stripped when thinking is active")
				}
				if hasTopP {
					t.Error("top_p should be stripped when thinking is active")
				}
				if hasTopK {
					t.Error("top_k should be stripped when thinking is active")
				}
			} else {
				if !hasTemp {
					t.Error("temperature should be present when thinking is inactive")
				}
				if !hasTopK {
					t.Error("top_k should be present when thinking is inactive")
				}
			}
		})
	}
}

// TestTopPMutuallyExclusiveWithTemperature verifies top_p is omitted when temperature is set.
func TestTopPMutuallyExclusiveWithTemperature(t *testing.T) {
	temp := 0.7
	topP := 0.9

	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	// Both set: temperature should be present, top_p should be dropped
	opts := &provider.GenerateOptions{
		Prompt:      types.Prompt{Text: "test"},
		Temperature: &temp,
		TopP:        &topP,
	}
	body := model.buildRequestBody(opts, false)

	if _, ok := body["temperature"]; !ok {
		t.Error("temperature should be present when both set")
	}
	if _, ok := body["top_p"]; ok {
		t.Error("top_p should be dropped when temperature is also set")
	}

	// Only top_p: should be included
	optsTopPOnly := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "test"},
		TopP:   &topP,
	}
	bodyTopPOnly := model.buildRequestBody(optsTopPOnly, false)
	if _, ok := bodyTopPOnly["top_p"]; !ok {
		t.Error("top_p should be present when temperature is not set")
	}
}

// --- disableParallelToolUse tests ---

// TestDisableParallelToolUse verifies the flag adds disable_parallel_tool_use to tool_choice.
func TestDisableParallelToolUse(t *testing.T) {
	regularTool := types.Tool{Name: "my_tool", Description: "A tool"}
	prov := New(Config{APIKey: "test-key"})

	// Without any explicit tool_choice: should create tool_choice with just the flag
	t.Run("creates tool_choice when none set", func(t *testing.T) {
		model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
			DisableParallelToolUse: true,
		})
		opts := &provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
			Tools:  []types.Tool{regularTool},
		}
		body := model.buildRequestBody(opts, false)
		tc, ok := body["tool_choice"].(map[string]interface{})
		if !ok {
			t.Fatalf("tool_choice should be map[string]interface{}, got %T", body["tool_choice"])
		}
		if tc["disable_parallel_tool_use"] != true {
			t.Errorf("disable_parallel_tool_use = %v, want true", tc["disable_parallel_tool_use"])
		}
	})

	// Not set: tool_choice should not have the flag
	t.Run("omitted when DisableParallelToolUse false", func(t *testing.T) {
		model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)
		opts := &provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
			Tools:  []types.Tool{regularTool},
		}
		body := model.buildRequestBody(opts, false)
		if tc, ok := body["tool_choice"].(map[string]interface{}); ok {
			if tc["disable_parallel_tool_use"] == true {
				t.Error("disable_parallel_tool_use should not be set")
			}
		}
		// No tool_choice at all is also fine
	})
}

// --- Streaming usage capture tests ---

// TestStreamingUsageCapturedFromMessageStart verifies that input/cache tokens from
// message_start are included in the finish chunk's Usage field.
func TestStreamingUsageCapturedFromMessageStart(t *testing.T) {
	sseData := "" +
		// message_start with input tokens
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-6\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":42,\"cache_read_input_tokens\":10,\"cache_creation_input_tokens\":5,\"output_tokens\":0}}}\n\n" +
		// text content
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
		// message_delta with output tokens and stop_reason
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":7}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	// First real chunk: text
	chunk1, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() text chunk error: %v", err)
	}
	if chunk1.Type != provider.ChunkTypeText || chunk1.Text != "Hello" {
		t.Errorf("unexpected chunk1: type=%v text=%q", chunk1.Type, chunk1.Text)
	}

	// Finish chunk: should have full usage
	finishChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() finish chunk error: %v", err)
	}
	if finishChunk.Type != provider.ChunkTypeFinish {
		t.Fatalf("expected finish chunk, got type=%v", finishChunk.Type)
	}
	if finishChunk.Usage == nil {
		t.Fatal("finish chunk Usage should not be nil")
	}

	// input_tokens=42, cache_read=10, cache_creation=5 → inputTotal=57
	// output_tokens=7 → totalTokens=64
	wantInput := int64(57)
	wantOutput := int64(7)
	wantTotal := int64(64)

	if finishChunk.Usage.InputTokens == nil || *finishChunk.Usage.InputTokens != wantInput {
		t.Errorf("InputTokens = %v, want %d", finishChunk.Usage.InputTokens, wantInput)
	}
	if finishChunk.Usage.OutputTokens == nil || *finishChunk.Usage.OutputTokens != wantOutput {
		t.Errorf("OutputTokens = %v, want %d", finishChunk.Usage.OutputTokens, wantOutput)
	}
	if finishChunk.Usage.TotalTokens == nil || *finishChunk.Usage.TotalTokens != wantTotal {
		t.Errorf("TotalTokens = %v, want %d", finishChunk.Usage.TotalTokens, wantTotal)
	}

	// Cache token breakdown
	if finishChunk.Usage.InputDetails == nil {
		t.Fatal("InputDetails should not be nil")
	}
	if *finishChunk.Usage.InputDetails.NoCacheTokens != 42 {
		t.Errorf("NoCacheTokens = %d, want 42", *finishChunk.Usage.InputDetails.NoCacheTokens)
	}
	if *finishChunk.Usage.InputDetails.CacheReadTokens != 10 {
		t.Errorf("CacheReadTokens = %d, want 10", *finishChunk.Usage.InputDetails.CacheReadTokens)
	}
	if *finishChunk.Usage.InputDetails.CacheWriteTokens != 5 {
		t.Errorf("CacheWriteTokens = %d, want 5", *finishChunk.Usage.InputDetails.CacheWriteTokens)
	}
}

// TestStreamingUsageWithNoMessageStart verifies graceful handling when message_start is absent.
func TestStreamingUsageWithNoMessageStart(t *testing.T) {
	sseData := "" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	stream.Next() // text chunk
	finishChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	if finishChunk.Usage == nil {
		t.Fatal("finish chunk Usage should not be nil even without message_start")
	}
	// inputTotal = 0+0+0 = 0, output = 3, total = 3
	if *finishChunk.Usage.OutputTokens != 3 {
		t.Errorf("OutputTokens = %d, want 3", *finishChunk.Usage.OutputTokens)
	}
	if *finishChunk.Usage.TotalTokens != 3 {
		t.Errorf("TotalTokens = %d, want 3", *finishChunk.Usage.TotalTokens)
	}
}

// --- Integration test placeholder (ANT-T05, ANT-T11) ---

// TestOutputConfigIntegration is a live integration test that verifies output_config.format
// works against the real Anthropic API. Skips when ANTHROPIC_API_KEY is not set.
func TestOutputConfigIntegration(t *testing.T) {
	t.Skip("Integration test: run manually with ANTHROPIC_API_KEY set")
}

// TestAutomaticCachingIntegration is a live integration test that verifies automatic caching
// works against the real Anthropic API. Skips when ANTHROPIC_API_KEY is not set.
func TestAutomaticCachingIntegration(t *testing.T) {
	t.Skip("Integration test: run manually with ANTHROPIC_API_KEY set")
}
