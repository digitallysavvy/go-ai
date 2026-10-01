package anthropic

import (
	"reflect"
	"sort"
	"testing"
)

// Ports amazon-bedrock-anthropic-provider.test.ts's transformRequestBody unit
// tests (ai@7.0.113) against transformRequestBodyWithBetas directly, since Go
// has no mocked AnthropicLanguageModel constructor to intercept config the
// way the TS tests do.

func TestTransformRequestBody_AddsVersionRemovesModel(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"messages":   []map[string]interface{}{{"role": "user", "content": "Hello"}},
		"max_tokens": 1024,
	}
	got := transformRequestBodyWithBetas(body, nil, false)

	if _, ok := got["model"]; ok {
		t.Error("model should be removed")
	}
	if got["anthropic_version"] != AnthropicVersion {
		t.Errorf("anthropic_version = %v, want %v", got["anthropic_version"], AnthropicVersion)
	}
	if got["max_tokens"] != 1024 {
		t.Errorf("max_tokens = %v, want 1024", got["max_tokens"])
	}
}

func TestTransformRequestBody_StripsStream(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"messages":   []map[string]interface{}{{"role": "user", "content": "Hello"}},
		"max_tokens": 1024,
		"stream":     true,
	}
	got := transformRequestBodyWithBetas(body, nil, true)
	if _, ok := got["stream"]; ok {
		t.Error("stream should be stripped")
	}
	if _, ok := got["anthropic_version"]; !ok {
		t.Error("anthropic_version should be present")
	}
}

func TestTransformRequestBody_StripsDisableParallelToolUse(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"max_tokens": 1024,
		"tool_choice": map[string]interface{}{
			"type":                      "auto",
			"disable_parallel_tool_use": true,
		},
	}
	got := transformRequestBodyWithBetas(body, nil, false)
	tc, ok := got["tool_choice"].(map[string]interface{})
	if !ok {
		t.Fatalf("tool_choice = %#v, want a map", got["tool_choice"])
	}
	if !reflect.DeepEqual(tc, map[string]interface{}{"type": "auto"}) {
		t.Errorf("tool_choice = %#v, want {type: auto}", tc)
	}
}

func TestTransformRequestBody_PreservesToolChoiceName(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"max_tokens": 1024,
		"tool_choice": map[string]interface{}{
			"type":                      "tool",
			"name":                      "my_tool",
			"disable_parallel_tool_use": true,
		},
	}
	got := transformRequestBodyWithBetas(body, nil, false)
	tc := got["tool_choice"].(map[string]interface{})
	if !reflect.DeepEqual(tc, map[string]interface{}{"type": "tool", "name": "my_tool"}) {
		t.Errorf("tool_choice = %#v", tc)
	}
}

func TestTransformRequestBody_MapsOldToolVersions(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"max_tokens": 1024,
		"tools": []map[string]interface{}{
			{"type": "bash_20241022", "name": "bash"},
			{"type": "text_editor_20241022", "name": "str_replace_editor"},
			{"type": "computer_20241022", "name": "computer"},
		},
	}
	got := transformRequestBodyWithBetas(body, nil, false)
	tools := got["tools"].([]map[string]interface{})
	want := []map[string]interface{}{
		{"type": "bash_20250124", "name": "bash"},
		{"type": "text_editor_20250728", "name": "str_replace_based_edit_tool"},
		{"type": "computer_20250124", "name": "computer"},
	}
	if !reflect.DeepEqual(tools, want) {
		t.Errorf("tools = %#v, want %#v", tools, want)
	}
}

func TestTransformRequestBody_AddsBetaForComputerUseTools(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"max_tokens": 1024,
		"tools":      []map[string]interface{}{{"type": "bash_20250124", "name": "bash"}},
	}
	got := transformRequestBodyWithBetas(body, nil, false)
	betas := got["anthropic_beta"].([]string)
	if !containsString(betas, "computer-use-2025-01-24") {
		t.Errorf("anthropic_beta = %v, want to contain computer-use-2025-01-24", betas)
	}
}

func TestTransformRequestBody_NoBetaWithoutTools(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"max_tokens": 1024,
	}
	got := transformRequestBodyWithBetas(body, nil, false)
	if got["anthropic_version"] != AnthropicVersion {
		t.Error("anthropic_version should be present")
	}
	if _, ok := got["anthropic_beta"]; ok {
		t.Errorf("anthropic_beta should be absent, got %v", got["anthropic_beta"])
	}
}

func TestTransformRequestBody_NoBetaForFunctionTools(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"max_tokens": 1024,
		"tools": []map[string]interface{}{{
			"type":         "function",
			"name":         "get_weather",
			"input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		}},
	}
	got := transformRequestBodyWithBetas(body, nil, false)
	if _, ok := got["anthropic_beta"]; ok {
		t.Errorf("anthropic_beta should be undefined for function tools, got %v", got["anthropic_beta"])
	}
}

func TestTransformRequestBody_RenamesBlockBindingField(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"max_tokens": 1024,
		"thinking": map[string]interface{}{
			"type":    "adaptive",
			"display": "summarized",
			"block_binding": map[string]interface{}{
				"prefix_mismatch_behavior": "drop_block",
			},
		},
	}
	got := transformRequestBodyWithBetas(body, []string{"thinking-binding-controls-2026-08-01"}, false)
	thinking := got["thinking"].(map[string]interface{})
	want := map[string]interface{}{
		"type":    "adaptive",
		"display": "summarized",
		"block_binding": map[string]interface{}{
			"mismatch_behavior": "drop_block",
		},
	}
	if !reflect.DeepEqual(thinking, want) {
		t.Errorf("thinking = %#v, want %#v", thinking, want)
	}
}

func TestTransformRequestBody_ThinkingUnchangedWithoutBlockBinding(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"max_tokens": 1024,
		"thinking": map[string]interface{}{
			"type":          "enabled",
			"budget_tokens": 2000,
		},
	}
	got := transformRequestBodyWithBetas(body, nil, false)
	want := map[string]interface{}{"type": "enabled", "budget_tokens": 2000}
	if !reflect.DeepEqual(got["thinking"], want) {
		t.Errorf("thinking = %#v, want %#v", got["thinking"], want)
	}
}

func TestTransformRequestBody_EagerInputStreamingTranslatesToBeta(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"max_tokens": 1024,
		"tools": []map[string]interface{}{
			{"name": "get_weather", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}, "eager_input_streaming": true},
			{"name": "get_time", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		},
	}
	got := transformRequestBodyWithBetas(body, nil, false)
	tools := got["tools"].([]map[string]interface{})
	wantTools := []map[string]interface{}{
		{"name": "get_weather", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{"name": "get_time", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	}
	if !reflect.DeepEqual(tools, wantTools) {
		t.Errorf("tools = %#v, want %#v", tools, wantTools)
	}
	betas := got["anthropic_beta"].([]string)
	if !reflect.DeepEqual(betas, []string{"fine-grained-tool-streaming-2025-05-14"}) {
		t.Errorf("anthropic_beta = %v", betas)
	}
}

func TestTransformRequestBody_NoFineGrainedBetaWithoutEagerFlag(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"max_tokens": 1024,
		"tools":      []map[string]interface{}{{"name": "get_weather", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}},
	}
	got := transformRequestBodyWithBetas(body, nil, false)
	if _, ok := got["anthropic_beta"]; ok {
		t.Errorf("anthropic_beta should be undefined, got %v", got["anthropic_beta"])
	}
}

func TestTransformRequestBody_EagerInputStreamingWithVersionRemap(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"max_tokens": 1024,
		"tools": []map[string]interface{}{
			{"name": "get_weather", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}, "eager_input_streaming": true},
			{"type": "bash_20241022", "name": "bash"},
		},
	}
	got := transformRequestBodyWithBetas(body, nil, false)
	tools := got["tools"].([]map[string]interface{})
	wantTools := []map[string]interface{}{
		{"name": "get_weather", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{"type": "bash_20250124", "name": "bash"},
	}
	if !reflect.DeepEqual(tools, wantTools) {
		t.Errorf("tools = %#v, want %#v", tools, wantTools)
	}
	betas := append([]string{}, got["anthropic_beta"].([]string)...)
	sort.Strings(betas)
	wantBetas := []string{"computer-use-2025-01-24", "fine-grained-tool-streaming-2025-05-14"}
	sort.Strings(wantBetas)
	if !reflect.DeepEqual(betas, wantBetas) {
		t.Errorf("anthropic_beta = %v, want (unordered) %v", betas, wantBetas)
	}
}

func TestTransformRequestBody_ToolSearchBeta(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"max_tokens": 1024,
		"tools": []map[string]interface{}{
			{"type": "tool_search_tool_regex_20251119", "name": "tool_search"},
			{"type": "tool_search_tool_bm25_20251119", "name": "tool_search_bm25"},
		},
	}
	got := transformRequestBodyWithBetas(body, nil, false)
	betas := got["anthropic_beta"].([]string)
	if !containsString(betas, "tool-search-tool-2025-10-19") {
		t.Errorf("anthropic_beta = %v, want to contain tool-search-tool-2025-10-19", betas)
	}
}

// TestTransformRequestBody_PassedBetasPreserved verifies that betas already
// collected by the shared Anthropic request builder (e.g. from prepareTools)
// are preserved in the output, in addition to any Bedrock-specific betas this
// transform adds.
func TestTransformRequestBody_PassedBetasPreserved(t *testing.T) {
	body := map[string]interface{}{
		"model":      "test-model-id",
		"max_tokens": 1024,
	}
	got := transformRequestBodyWithBetas(body, []string{"structured-outputs-2025-11-13"}, false)
	betas := got["anthropic_beta"].([]string)
	if !reflect.DeepEqual(betas, []string{"structured-outputs-2025-11-13"}) {
		t.Errorf("anthropic_beta = %v, want [structured-outputs-2025-11-13]", betas)
	}
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
