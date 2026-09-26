package anthropic

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

// ---------------------------------------------------------------------------
// Safeguards (TS anthropic-language-model.test.ts "safeguards" describe block)
// ---------------------------------------------------------------------------

// TestSafeguardsRequestBodyAndBeta ports "should send safeguards in request
// body and add the beta".
func TestSafeguardsRequestBodyAndBeta(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
		Safeguards: []Safeguard{{
			Type:              "dangerous_tool_use",
			ClassifierContext: map[string]interface{}{"v": 1, "permission_mode": "auto"},
		}},
	})

	body := model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}}, false)

	safeguards, ok := body["safeguards"].([]map[string]interface{})
	if !ok || len(safeguards) != 1 {
		t.Fatalf("safeguards = %#v, want one entry", body["safeguards"])
	}
	if safeguards[0]["type"] != "dangerous_tool_use" {
		t.Errorf("safeguards[0].type = %v, want dangerous_tool_use", safeguards[0]["type"])
	}
	cc, ok := safeguards[0]["classifier_context"].(map[string]interface{})
	if !ok || cc["v"] != 1 || cc["permission_mode"] != "auto" {
		t.Errorf("safeguards[0].classifier_context = %#v", safeguards[0]["classifier_context"])
	}

	if h := model.getBetaHeaders(); !strings.Contains(h, BetaHeaderDangerousToolUse) {
		t.Fatalf("anthropic-beta = %q, want to contain %q", h, BetaHeaderDangerousToolUse)
	}
}

// TestSafeguardsAbsentByDefault ports "should not send safeguards or the beta
// when the option is absent".
func TestSafeguardsAbsentByDefault(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	body := model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}}, false)
	if _, ok := body["safeguards"]; ok {
		t.Errorf("safeguards should be absent, got %#v", body["safeguards"])
	}
	if h := model.getBetaHeaders(); strings.Contains(h, "dangerous-tool-use") {
		t.Errorf("anthropic-beta = %q, should not contain dangerous-tool-use", h)
	}
}

// TestSafeguardResultsProviderMetadata ports "should expose safeguard_results
// as provider metadata" (non-streaming).
func TestSafeguardResultsProviderMetadata(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	response := anthropicResponse{
		ID:         "msg_123",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-3-haiku-20240307",
		StopReason: "tool_use",
		Content: []anthropicContent{
			{Type: "tool_use", ID: "toolu_01", Name: "Bash", Input: map[string]interface{}{"command": "echo hello"}},
		},
		Usage: anthropicUsage{InputTokens: 100, OutputTokens: 50},
		SafeguardResults: json.RawMessage(`[{
			"type": "dangerous_tool_use",
			"status": {
				"type": "available",
				"tool_uses": {"toolu_01": {"type": "evaluated", "outcome": "flagged", "explanation": "[Data Exfiltration]"}}
			}
		}]`),
	}

	result := model.convertResponse(response, false, nil)
	meta, ok := result.ProviderMetadata["anthropic"].(map[string]interface{})
	if !ok {
		t.Fatalf("provider metadata missing anthropic object: %#v", result.ProviderMetadata)
	}
	sgResults, ok := meta["safeguardResults"].([]interface{})
	if !ok || len(sgResults) != 1 {
		t.Fatalf("safeguardResults = %#v, want one entry", meta["safeguardResults"])
	}
	entry, ok := sgResults[0].(map[string]interface{})
	if !ok || entry["type"] != "dangerous_tool_use" {
		t.Fatalf("safeguardResults[0] = %#v", sgResults[0])
	}
}

// TestSafeguardResultsOmittedWhenAbsent ports "should omit safeguardResults
// when the response has none".
func TestSafeguardResultsOmittedWhenAbsent(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	response := anthropicResponse{
		ID:         "msg_123",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-3-haiku-20240307",
		StopReason: "end_turn",
		Content:    []anthropicContent{{Type: "text", Text: "hi"}},
		Usage:      anthropicUsage{InputTokens: 1, OutputTokens: 1},
	}

	result := model.convertResponse(response, false, nil)
	if meta, ok := result.ProviderMetadata["anthropic"].(map[string]interface{}); ok {
		if _, has := meta["safeguardResults"]; has {
			t.Fatalf("safeguardResults should be absent, got %#v", meta["safeguardResults"])
		}
	}
}

// TestSafeguardResultsFromMessageDeltaStreaming ports "should expose the last
// non-null safeguard_results from message_delta".
func TestSafeguardResultsFromMessageDeltaStreaming(t *testing.T) {
	sseData := "" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-6\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null,\"safeguard_results\":null},\"usage\":{\"output_tokens\":50}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null,\"safeguard_results\":[{\"type\":\"dangerous_tool_use\",\"status\":{\"type\":\"available\"}}]},\"usage\":{\"output_tokens\":50}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	textChunk, err := stream.Next()
	if err != nil || textChunk.Type != provider.ChunkTypeText {
		t.Fatalf("text chunk = %#v, err = %v", textChunk, err)
	}
	finishChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() finish chunk error: %v", err)
	}
	if finishChunk.Type != provider.ChunkTypeFinish {
		t.Fatalf("chunk type = %v, want finish", finishChunk.Type)
	}

	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(finishChunk.ProviderMetadata, &meta); err != nil {
		t.Fatalf("decode provider metadata: %v", err)
	}
	sgResults, ok := meta["anthropic"]["safeguardResults"].([]interface{})
	if !ok || len(sgResults) != 1 {
		t.Fatalf("safeguardResults = %#v, want the last non-null value", meta["anthropic"]["safeguardResults"])
	}
}

// ---------------------------------------------------------------------------
// Thinking block_binding (TS "should serialize thinking binding controls...")
// ---------------------------------------------------------------------------

// TestThinkingBlockBindingAlone ports "should serialize thinking binding
// controls and add the beta header": blockBinding with no thinking type sends
// only {block_binding:{...}} with no "type" key.
func TestThinkingBlockBindingAlone(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeFable5, &ModelOptions{
		Thinking: &ThinkingConfig{
			BlockBinding: &ThinkingBlockBinding{PrefixMismatchBehavior: "drop_block"},
		},
	})

	body := model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}}, false)
	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("thinking = %#v, want a map", body["thinking"])
	}
	if _, hasType := thinking["type"]; hasType {
		t.Errorf("thinking.type should be absent for blockBinding-only, got %v", thinking["type"])
	}
	blockBinding, ok := thinking["block_binding"].(map[string]interface{})
	if !ok || blockBinding["prefix_mismatch_behavior"] != "drop_block" {
		t.Fatalf("thinking.block_binding = %#v", thinking["block_binding"])
	}
	if h := model.getBetaHeaders(); !strings.Contains(h, BetaHeaderThinkingBindingControls) {
		t.Fatalf("anthropic-beta = %q, want to contain %q", h, BetaHeaderThinkingBindingControls)
	}
}

// TestThinkingAdaptiveWithBlockBinding ports "should serialize strict thinking
// binding controls with adaptive thinking" (claude-fable-5-1, maxOutputTokens:4096).
func TestThinkingAdaptiveWithBlockBinding(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeFable5_1, &ModelOptions{
		Thinking: &ThinkingConfig{
			Type:         ThinkingTypeAdaptive,
			BlockBinding: &ThinkingBlockBinding{PrefixMismatchBehavior: "error"},
		},
	})

	maxTokens := 4096
	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "Hello"},
		MaxTokens: &maxTokens,
	}, false)

	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("thinking = %#v, want a map", body["thinking"])
	}
	if thinking["type"] != "adaptive" {
		t.Errorf("thinking.type = %v, want adaptive", thinking["type"])
	}
	blockBinding, ok := thinking["block_binding"].(map[string]interface{})
	if !ok || blockBinding["prefix_mismatch_behavior"] != "error" {
		t.Fatalf("thinking.block_binding = %#v", thinking["block_binding"])
	}
	// TS inline snapshot: max_tokens stays 4096 (adaptive thinking has no
	// budget_tokens contribution).
	if body["max_tokens"] != 4096 {
		t.Errorf("max_tokens = %v, want 4096", body["max_tokens"])
	}
	if h := model.getBetaHeaders(); !strings.Contains(h, BetaHeaderThinkingBindingControls) {
		t.Fatalf("anthropic-beta = %q, want to contain %q", h, BetaHeaderThinkingBindingControls)
	}
}

// ---------------------------------------------------------------------------
// inputTransformations metadata (TS e4292e7)
// ---------------------------------------------------------------------------

func TestInputTransformationsProviderMetadataGenerate(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeFable5_1, nil)

	response := anthropicResponse{
		ID:         "msg_123",
		Type:       "message",
		Role:       "assistant",
		Model:      ClaudeFable5_1,
		StopReason: "end_turn",
		Content:    []anthropicContent{{Type: "text", Text: "hi"}},
		Usage:      anthropicUsage{InputTokens: 1, OutputTokens: 1},
		InputTransformations: json.RawMessage(`[
			{"type": "thinking_block_dropped", "path": "messages.0.content.0", "reason": "prefix_mismatch"}
		]`),
	}

	result := model.convertResponse(response, false, nil)
	meta, ok := result.ProviderMetadata["anthropic"].(map[string]interface{})
	if !ok {
		t.Fatalf("provider metadata missing anthropic object: %#v", result.ProviderMetadata)
	}
	transformations, ok := meta["inputTransformations"].([]interface{})
	if !ok || len(transformations) != 1 {
		t.Fatalf("inputTransformations = %#v, want one entry", meta["inputTransformations"])
	}
	entry, ok := transformations[0].(map[string]interface{})
	if !ok || entry["type"] != "thinking_block_dropped" || entry["reason"] != "prefix_mismatch" {
		t.Fatalf("inputTransformations[0] = %#v", transformations[0])
	}
}

func TestInputTransformationsFromStreamMessageStartAndDelta(t *testing.T) {
	sseData := "" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-fable-5-1\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0},\"input_transformations\":[{\"type\":\"thinking_block_dropped\",\"path\":\"messages.0\",\"reason\":\"prefix_mismatch\"}]}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":1}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	textChunk, err := stream.Next()
	if err != nil || textChunk.Type != provider.ChunkTypeText {
		t.Fatalf("text chunk = %#v, err = %v", textChunk, err)
	}
	finishChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() finish chunk error: %v", err)
	}
	if finishChunk.Type != provider.ChunkTypeFinish {
		t.Fatalf("chunk type = %v, want finish", finishChunk.Type)
	}

	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(finishChunk.ProviderMetadata, &meta); err != nil {
		t.Fatalf("decode provider metadata: %v", err)
	}
	transformations, ok := meta["anthropic"]["inputTransformations"].([]interface{})
	if !ok || len(transformations) != 1 {
		t.Fatalf("inputTransformations = %#v", meta["anthropic"]["inputTransformations"])
	}
}

// ---------------------------------------------------------------------------
// Spliced stream rejection (TS 8b96941)
// ---------------------------------------------------------------------------

// TestSplicedStreamRejected ports the "Received message_start for message X
// while message Y is still open." behavior: a second message_start with a
// different ID while a message is open is dropped, with an error chunk in its
// place, and everything after it is discarded.
func TestSplicedStreamRejected(t *testing.T) {
	sseData := "" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_A\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-6\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_B\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-6\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"spliced\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	textChunk, err := stream.Next()
	if err != nil || textChunk.Type != provider.ChunkTypeText || textChunk.Text != "hi" {
		t.Fatalf("text chunk = %#v, err = %v", textChunk, err)
	}

	// The spliced message_start surfaces as an error CHUNK within the stream
	// (matching TS, which enqueues a stream part rather than throwing / erroring
	// the ReadableStream) — not a fatal Go error. This lets consumers observe it
	// via ChunkTypeError the same way they would any other mid-stream provider
	// error (see pkg/ai/stream.go's ChunkTypeError handling and
	// openai/language_model.go's analogous outputStarted convention).
	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("expected a nil error alongside the spliced-stream error chunk, got %v", err)
	}
	if chunk == nil || chunk.Type != provider.ChunkTypeError {
		t.Fatalf("chunk = %#v, want a ChunkTypeError chunk", chunk)
	}
	wantMsg := `Received message_start for message "msg_B" while message "msg_A" is still open.`
	if chunk.Text != wantMsg {
		t.Errorf("Text = %q, want %q", chunk.Text, wantMsg)
	}

	// Everything after the spliced message_start (including the "spliced" text
	// delta and message_stop) is discarded; the stream ends without a finish
	// chunk, exactly as TS never re-enqueues after hasInvalidMessageSequence.
	next, err2 := stream.Next()
	if next != nil || err2 != io.EOF {
		t.Fatalf("expected (nil, io.EOF) after the spliced-stream error, got chunk=%#v err=%v", next, err2)
	}
}

// TestNonSplicedRepeatMessageStartIgnored verifies that a duplicate
// message_start with the SAME id is ignored rather than treated as spliced.
func TestNonSplicedRepeatMessageStartIgnored(t *testing.T) {
	sseData := "" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_A\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-6\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_A\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-6\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":1}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)
	textChunk, err := stream.Next()
	if err != nil || textChunk.Type != provider.ChunkTypeText || textChunk.Text != "hi" {
		t.Fatalf("text chunk = %#v, err = %v", textChunk, err)
	}
	finishChunk, err := stream.Next()
	if err != nil || finishChunk.Type != provider.ChunkTypeFinish {
		t.Fatalf("finish chunk = %#v, err = %v", finishChunk, err)
	}
}

// ---------------------------------------------------------------------------
// thinking_tokens usage → ReasoningTokens (TS e29788d)
// ---------------------------------------------------------------------------

func TestThinkingTokensUsageGenerate(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeOpus4_6, nil)

	response := anthropicResponse{
		ID:         "msg_123",
		Type:       "message",
		Role:       "assistant",
		Model:      ClaudeOpus4_6,
		StopReason: "end_turn",
		Content:    []anthropicContent{{Type: "text", Text: "hi"}},
		Usage: anthropicUsage{
			InputTokens:  10,
			OutputTokens: 100,
			OutputTokensDetails: &anthropicOutputTokensDetails{
				ThinkingTokens: intPtr(60),
			},
		},
	}

	result := model.convertResponse(response, false, nil)
	if result.Usage.OutputDetails == nil || result.Usage.OutputDetails.ReasoningTokens == nil {
		t.Fatalf("OutputDetails.ReasoningTokens missing: %#v", result.Usage.OutputDetails)
	}
	if *result.Usage.OutputDetails.ReasoningTokens != 60 {
		t.Errorf("ReasoningTokens = %d, want 60", *result.Usage.OutputDetails.ReasoningTokens)
	}
	if result.Usage.OutputDetails.TextTokens == nil || *result.Usage.OutputDetails.TextTokens != 40 {
		t.Errorf("TextTokens = %v, want 40 (100 output - 60 thinking)", result.Usage.OutputDetails.TextTokens)
	}
}

func TestThinkingTokensUsageStreamingMessageDelta(t *testing.T) {
	sseData := "" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-opus-4-6\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":100,\"output_tokens_details\":{\"thinking_tokens\":60}}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)
	textChunk, err := stream.Next()
	if err != nil || textChunk.Type != provider.ChunkTypeText {
		t.Fatalf("text chunk = %#v, err = %v", textChunk, err)
	}
	finishChunk, err := stream.Next()
	if err != nil || finishChunk.Type != provider.ChunkTypeFinish {
		t.Fatalf("finish chunk = %#v, err = %v", finishChunk, err)
	}
	if finishChunk.Usage == nil || finishChunk.Usage.OutputDetails == nil || finishChunk.Usage.OutputDetails.ReasoningTokens == nil {
		t.Fatalf("Usage.OutputDetails.ReasoningTokens missing: %#v", finishChunk.Usage)
	}
	if *finishChunk.Usage.OutputDetails.ReasoningTokens != 60 {
		t.Errorf("ReasoningTokens = %d, want 60", *finishChunk.Usage.OutputDetails.ReasoningTokens)
	}
}

// ---------------------------------------------------------------------------
// Unknown-model max output tokens (TS anthropic-unknown-model-max-output-tokens.test.ts)
// ---------------------------------------------------------------------------

func newUnknownModelServer(t *testing.T, modelID string) (*httptest.Server, *map[string]interface{}) {
	t.Helper()
	var reqBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_test", "type": "message", "role": "assistant",
			"model": "` + modelID + `",
			"content": [{"type": "text", "text": "Hello!"}],
			"stop_reason": "end_turn", "stop_sequence": null,
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`))
	}))
	return srv, &reqBody
}

// TestUnknownModelWarnsDefaultMaxOutputTokens ports "should warn when using
// the default max output token limit" (non-Claude unknown model → 4096).
func TestUnknownModelWarnsDefaultMaxOutputTokens(t *testing.T) {
	srv, reqBody := newUnknownModelServer(t, "future-model")
	defer srv.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := prov.LanguageModel("future-model")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}

	if (*reqBody)["max_tokens"] != float64(4096) {
		t.Errorf("max_tokens = %v, want 4096", (*reqBody)["max_tokens"])
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Type != "compatibility" || result.Warnings[0].Feature != "maxOutputTokens" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	wantDetails := `The model "future-model" is unknown. The max output tokens have been limited to 4096. Set maxOutputTokens explicitly to override this limit.`
	if result.Warnings[0].Details != wantDetails {
		t.Errorf("warning details = %q, want %q", result.Warnings[0].Details, wantDetails)
	}
}

// TestUnknownModelNoWarningWhenMaxTokensProvided ports "should not warn when
// max output tokens are provided".
func TestUnknownModelNoWarningWhenMaxTokensProvided(t *testing.T) {
	srv, reqBody := newUnknownModelServer(t, "future-model")
	defer srv.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := prov.LanguageModel("future-model")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	maxTokens := 123456
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "Hello"},
		MaxTokens: &maxTokens,
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if (*reqBody)["max_tokens"] != float64(123456) {
		t.Errorf("max_tokens = %v, want 123456", (*reqBody)["max_tokens"])
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", result.Warnings)
	}
}

// TestUnknownClaudeModelUsesCurrentGenDefault ports "should use the
// current-generation default and warn for an unknown Claude model".
func TestUnknownClaudeModelUsesCurrentGenDefault(t *testing.T) {
	srv, reqBody := newUnknownModelServer(t, "claude-future-9")
	defer srv.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := prov.LanguageModel("claude-future-9")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if (*reqBody)["max_tokens"] != float64(128000) {
		t.Errorf("max_tokens = %v, want 128000", (*reqBody)["max_tokens"])
	}
	wantDetails := `The model "claude-future-9" is unknown. The max output tokens have been limited to 128000. Set maxOutputTokens explicitly to override this limit.`
	if len(result.Warnings) != 1 || result.Warnings[0].Details != wantDetails {
		t.Fatalf("warnings = %#v, want details %q", result.Warnings, wantDetails)
	}
}

// ---------------------------------------------------------------------------
// fallbacks: 'default' (TS anthropic-language-model.test.ts "fallbacks"
// describe block, cbdc990). The array-form fallbacks + empty-array cases are
// already covered by TestOpus5FallbacksAndEffortLowering-adjacent coverage in
// provider_updates_test.go; this covers the 'default' string variant.
// ---------------------------------------------------------------------------

// TestFallbacksDefaultSendsStringAndBeta ports "should pass fallbacks
// 'default' through and add the 2026-07-01 beta header".
func TestFallbacksDefaultSendsStringAndBeta(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeOpus5, &ModelOptions{
		FallbacksDefault: true,
	})

	body := model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}}, false)
	if got, ok := body["fallbacks"].(string); !ok || got != "default" {
		t.Fatalf("fallbacks = %#v, want \"default\"", body["fallbacks"])
	}
	if h := model.getBetaHeaders(); !strings.Contains(h, BetaHeaderServerSideFallbackDefault) {
		t.Fatalf("anthropic-beta = %q, want to contain %q", h, BetaHeaderServerSideFallbackDefault)
	}
	if h := model.getBetaHeaders(); strings.Contains(h, BetaHeaderServerSideFallback) {
		t.Fatalf("anthropic-beta = %q, should not also contain the array-form beta %q", h, BetaHeaderServerSideFallback)
	}
}

// TestFallbacksDefaultTakesPrecedenceOverArray ports the FallbacksDefault
// precedence documented on ModelOptions: FallbacksDefault wins over Fallbacks
// when both are set.
func TestFallbacksDefaultTakesPrecedenceOverArray(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	maxTokens := 1000
	model := NewLanguageModel(prov, ClaudeOpus5, &ModelOptions{
		FallbacksDefault: true,
		Fallbacks:        []FallbackConfig{{Model: ClaudeOpus4_8, MaxTokens: &maxTokens}},
	})

	body := model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}}, false)
	if got, ok := body["fallbacks"].(string); !ok || got != "default" {
		t.Fatalf("fallbacks = %#v, want \"default\" (FallbacksDefault takes precedence)", body["fallbacks"])
	}
	if h := model.getBetaHeaders(); !strings.Contains(h, BetaHeaderServerSideFallbackDefault) {
		t.Fatalf("anthropic-beta = %q, want to contain %q", h, BetaHeaderServerSideFallbackDefault)
	}
}
