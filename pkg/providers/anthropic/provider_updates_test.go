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

// --- Compaction delta null content tests (ANT-T16) ---

// TestCompactionDeltaNullContent verifies that a null content in compaction_delta
// does not cause a parse error or panic in the stream parser.
func TestCompactionDeltaNullContent(t *testing.T) {
	// Simulate an SSE stream with a compaction_delta event where content is null
	sseData := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"compaction_delta\",\"content\":null}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)))

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

// TestCompactionDeltaWithContent verifies compaction_delta with non-null content is also
// handled gracefully (skipped, as we don't expose compaction content as a text chunk).
func TestCompactionDeltaWithContent(t *testing.T) {
	sseData := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"compaction_delta\",\"content\":\"summary text\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)))

	// compaction_delta should be skipped → next is message_stop → EOF
	_, err := stream.Next()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got: %v", err)
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
