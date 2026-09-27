package anthropic

import (
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// This file ports the TS anthropic-language-model.test.ts pattern of passing
// providerOptions.anthropic per doGenerate/doStream call (91 occurrences in
// that file), which the Go SDK previously only supported at construction
// time via ModelOptions. It exercises pkg/providers/anthropic/call_options.go
// directly through LanguageModel.prepareRequest.

func genOpts(text string, providerOptions map[string]interface{}) *provider.GenerateOptions {
	return &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: text},
		ProviderOptions: providerOptions,
	}
}

// TestPerCall_ConstructionDefaultsUsedWhenNotOverridden verifies
// construction-time ModelOptions remain the effective defaults when a call
// supplies no providerOptions at all.
func TestPerCall_ConstructionDefaultsUsedWhenNotOverridden(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{Effort: EffortHigh})

	body := model.buildRequestBody(genOpts("hi", nil), false)

	oc, ok := body["output_config"].(map[string]interface{})
	if !ok || oc["effort"] != "high" {
		t.Fatalf("output_config = %#v, want {effort: high} from construction default", body["output_config"])
	}
}

// TestPerCall_CallWinsOverConstructionDefault verifies a per-call
// providerOptions.anthropic field overrides the construction-time default
// for that field.
func TestPerCall_CallWinsOverConstructionDefault(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{Effort: EffortLow})

	body := model.buildRequestBody(genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{"effort": "high"},
	}), false)

	oc, ok := body["output_config"].(map[string]interface{})
	if !ok || oc["effort"] != "high" {
		t.Fatalf("output_config = %#v, want {effort: high} (call should win)", body["output_config"])
	}
}

// TestPerCall_OtherConstructionFieldsSurviveCallOverride verifies fields the
// call does not mention keep their construction-time value even when other
// fields are overridden (field-by-field merge, not whole-object replace).
func TestPerCall_OtherConstructionFieldsSurviveCallOverride(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
		Effort:      EffortLow,
		ServiceTier: "auto",
	})

	body := model.buildRequestBody(genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{"effort": "high"},
	}), false)

	oc, ok := body["output_config"].(map[string]interface{})
	if !ok || oc["effort"] != "high" {
		t.Fatalf("output_config = %#v, want {effort: high}", body["output_config"])
	}
	if body["service_tier"] != "auto" {
		t.Fatalf("service_tier = %v, want auto (construction default preserved)", body["service_tier"])
	}
}

// TestPerCall_Thinking ports thinking-related providerOptions tests: a
// per-call thinking config with no construction-time ModelOptions at all.
func TestPerCall_Thinking(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	body := model.buildRequestBody(genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{
			"thinking": map[string]interface{}{"type": "enabled", "budgetTokens": float64(2048)},
		},
	}), false)

	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok || thinking["type"] != "enabled" || thinking["budget_tokens"] != 2048 {
		t.Fatalf("thinking = %#v, want {type: enabled, budget_tokens: 2048}", body["thinking"])
	}
}

// TestPerCall_SendReasoning verifies providerOptions.anthropic.sendReasoning
// flows per call (strips reasoning content parts from history when false).
func TestPerCall_SendReasoning(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ReasoningContent{Text: "thinking...", Signature: "sig"},
				types.TextContent{Text: "answer"},
			}},
		}},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{"sendReasoning": false},
		},
	}

	body := model.buildRequestBody(opts, false)
	messages, ok := body["messages"].([]map[string]interface{})
	if !ok {
		t.Fatalf("messages = %#v, want []map[string]interface{}", body["messages"])
	}
	for _, msg := range messages {
		content, _ := msg["content"].([]map[string]interface{})
		for _, c := range content {
			if c["type"] == "thinking" || c["type"] == "redacted_thinking" {
				t.Fatalf("expected reasoning content to be stripped, found %#v", c)
			}
		}
	}
}

// TestPerCall_DisableParallelToolUse verifies the per-call
// providerOptions.anthropic.disableParallelToolUse flag reaches tool_choice.
func TestPerCall_DisableParallelToolUse(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	opts := genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{"disableParallelToolUse": true},
	})
	opts.Tools = []types.Tool{{Name: "get_weather", Parameters: map[string]interface{}{"type": "object"}}}

	body := model.buildRequestBody(opts, false)
	tc, ok := body["tool_choice"].(map[string]interface{})
	if !ok || tc["disable_parallel_tool_use"] != true {
		t.Fatalf("tool_choice = %#v, want disable_parallel_tool_use: true", body["tool_choice"])
	}
}

// TestPerCall_CacheControl verifies providerOptions.anthropic.cacheControl
// flows to the request body per call.
func TestPerCall_CacheControl(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	body := model.buildRequestBody(genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{
			"cacheControl": map[string]interface{}{"type": "ephemeral", "ttl": "1h"},
		},
	}), false)

	cc, ok := body["cache_control"].(map[string]interface{})
	if !ok {
		// The wire body stores *CacheControlOption directly; accept either
		// shape depending on how the request builder marshals it.
		if raw, ok2 := body["cache_control"].(*CacheControlOption); ok2 {
			if raw.Type != "ephemeral" || raw.TTL != "1h" {
				t.Fatalf("cache_control = %#v", raw)
			}
			return
		}
		t.Fatalf("cache_control = %#v, want ephemeral/1h", body["cache_control"])
	}
	if cc["type"] != "ephemeral" || cc["ttl"] != "1h" {
		t.Fatalf("cache_control = %#v, want ephemeral/1h", cc)
	}
}

// TestPerCall_MCPServers verifies providerOptions.anthropic.mcpServers flows
// per call and adds the mcp-client beta.
func TestPerCall_MCPServers(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	opts := genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{
			"mcpServers": []interface{}{
				map[string]interface{}{
					"type":               "url",
					"name":               "my-server",
					"url":                "https://mcp.example.com/sse",
					"authorizationToken": "tok",
				},
			},
		},
	})

	req, err := model.prepareRequest(opts, false)
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	servers, ok := req.body["mcp_servers"].([]map[string]interface{})
	if !ok || len(servers) != 1 {
		t.Fatalf("mcp_servers = %#v, want one entry", req.body["mcp_servers"])
	}
	if servers[0]["name"] != "my-server" || servers[0]["authorization_token"] != "tok" {
		t.Fatalf("mcp_servers[0] = %#v", servers[0])
	}
	found := false
	for _, b := range req.betas {
		if b == BetaHeaderMCPClient {
			found = true
		}
	}
	if !found {
		t.Fatalf("betas = %v, want to contain %q", req.betas, BetaHeaderMCPClient)
	}
}

// TestPerCall_Container verifies providerOptions.anthropic.container (with
// skills) flows per call and adds the skills betas.
func TestPerCall_Container(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	opts := genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{
			"container": map[string]interface{}{
				"skills": []interface{}{
					map[string]interface{}{"type": "anthropic", "skillId": "pptx"},
				},
			},
		},
	})
	opts.Tools = []types.Tool{{Name: "anthropic.code_execution_20250825", Parameters: map[string]interface{}{"type": "object"}}}

	req, err := model.prepareRequest(opts, false)
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	container, ok := req.body["container"].(map[string]interface{})
	if !ok {
		t.Fatalf("container = %#v, want an object with skills", req.body["container"])
	}
	skills, ok := container["skills"].([]map[string]interface{})
	if !ok || len(skills) != 1 || skills[0]["skill_id"] != "pptx" {
		t.Fatalf("container.skills = %#v", container["skills"])
	}
	for _, want := range []string{BetaHeaderCodeExecution20250825, BetaHeaderSkills, BetaHeaderFilesAPI} {
		found := false
		for _, b := range req.betas {
			if b == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("betas = %v, want to contain %q", req.betas, want)
		}
	}
}

// TestPerCall_ContainerSkillsWarningDetectedFromPerCallOptions is a
// regression test: detectSkillsWarning previously read m.options.Container
// directly (construction-time only), so a container/skills configuration
// supplied only via providerOptions.anthropic (no construction-time
// ModelOptions) never triggered the "code execution tool is required when
// using skills" warning. It must now resolve the per-call merged options.
func TestPerCall_ContainerSkillsWarningDetectedFromPerCallOptions(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil) // no construction-time Container

	opts := genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{
			"container": map[string]interface{}{
				"skills": []interface{}{
					map[string]interface{}{"type": "anthropic", "skillId": "pptx"},
				},
			},
		},
	})
	// No code execution tool supplied.

	req, err := model.prepareRequest(opts, false)
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	found := false
	for _, w := range req.warnings {
		if w.Type == "other" && w.Message == "code execution tool is required when using skills" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %#v, want the missing-code-execution-tool warning", req.warnings)
	}
}

// TestPerCall_ContextManagement verifies
// providerOptions.anthropic.contextManagement flows per call.
func TestPerCall_ContextManagement(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	opts := genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{
			"contextManagement": map[string]interface{}{
				"edits": []interface{}{
					map[string]interface{}{
						"type":            "clear_tool_uses_20250919",
						"clearToolInputs": true,
						"excludeTools":    []interface{}{"keep_me"},
					},
				},
			},
		},
	})

	req, err := model.prepareRequest(opts, false)
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	cm, ok := req.body["context_management"].(*ContextManagement)
	if !ok || len(cm.Edits) != 1 {
		t.Fatalf("context_management = %#v, want one edit", req.body["context_management"])
	}
	edit, ok := cm.Edits[0].(*ClearToolUsesEdit)
	if !ok || edit.ClearToolInputs == nil || !*edit.ClearToolInputs || len(edit.ExcludeTools) != 1 || edit.ExcludeTools[0] != "keep_me" {
		t.Fatalf("edit = %#v", cm.Edits[0])
	}
	found := false
	for _, b := range req.betas {
		if b == BetaHeaderContextManagement {
			found = true
		}
	}
	if !found {
		t.Fatalf("betas = %v, want to contain %q", req.betas, BetaHeaderContextManagement)
	}
}

// TestPerCall_CompactionAndContextManagementMutuallyExclusive ports "should
// reject compaction together with context management", but with both
// options supplied per call (not construction) — the gap this slice closes.
func TestPerCall_CompactionAndContextManagementMutuallyExclusive(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	opts := genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{
			"compaction": map[string]interface{}{"type": "summarize"},
			"contextManagement": map[string]interface{}{
				"edits": []interface{}{map[string]interface{}{"type": "clear_tool_uses_20250919"}},
			},
		},
	})

	_, err := model.prepareRequest(opts, false)
	if err == nil {
		t.Fatal("expected an error")
	}
	var invalidArg *providererrors.InvalidArgumentError
	if !errorsAs(err, &invalidArg) {
		t.Fatalf("error = %v, want *InvalidArgumentError", err)
	}
	want := "Anthropic provider options `compaction` and `contextManagement` cannot be used together."
	if invalidArg.Message != want {
		t.Fatalf("Message = %q, want %q", invalidArg.Message, want)
	}
}

// TestPerCall_StructuredOutputMode verifies
// providerOptions.anthropic.structuredOutputMode flows per call.
func TestPerCall_StructuredOutputMode(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	opts := genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{"structuredOutputMode": "jsonTool"},
	})
	opts.ResponseFormat = &provider.ResponseFormat{
		Type:   "json",
		Schema: map[string]interface{}{"type": "object"},
	}

	req, err := model.prepareRequest(opts, false)
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	if !req.usesJSONResponseTool {
		t.Fatal("expected usesJSONResponseTool = true when structuredOutputMode = jsonTool")
	}
}

// TestPerCall_ToolStreaming verifies
// providerOptions.anthropic.toolStreaming=false disables the default eager
// input streaming per call.
func TestPerCall_ToolStreaming(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	opts := genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{"toolStreaming": false},
	})
	opts.Tools = []types.Tool{{Name: "get_weather", Parameters: map[string]interface{}{"type": "object"}}}

	req, err := model.prepareRequest(opts, true)
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	tools, ok := req.body["tools"].([]map[string]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", req.body["tools"])
	}
	if v, present := tools[0]["eager_input_streaming"]; present && v == true {
		t.Fatalf("eager_input_streaming = %#v, want unset/false when toolStreaming=false", v)
	}
}

// TestPerCall_TaskBudget verifies providerOptions.anthropic.taskBudget flows
// per call and adds the task-budgets beta.
func TestPerCall_TaskBudget(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	opts := genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{
			"taskBudget": map[string]interface{}{"type": "tokens", "total": float64(50000)},
		},
	})

	req, err := model.prepareRequest(opts, false)
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	oc, ok := req.body["output_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("output_config = %#v", req.body["output_config"])
	}
	tb, ok := oc["task_budget"].(map[string]interface{})
	if !ok || tb["total"] != 50000 {
		t.Fatalf("task_budget = %#v, want total: 50000", oc["task_budget"])
	}
	found := false
	for _, b := range req.betas {
		if b == BetaHeaderTaskBudgets {
			found = true
		}
	}
	if !found {
		t.Fatalf("betas = %v, want to contain %q", req.betas, BetaHeaderTaskBudgets)
	}
}

// TestPerCall_Metadata verifies providerOptions.anthropic.metadata.userId
// flows to the request body per call (previously the only per-call field
// supported, via the now-removed callMetadataUserID helper).
func TestPerCall_Metadata(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	body := model.buildRequestBody(genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{
			"metadata": map[string]interface{}{"userId": "user-123"},
		},
	}), false)

	md, ok := body["metadata"].(map[string]interface{})
	if !ok || md["user_id"] != "user-123" {
		t.Fatalf("metadata = %#v, want user_id: user-123", body["metadata"])
	}
}

// TestPerCall_Safeguards verifies providerOptions.anthropic.safeguards flows
// per call and adds the dangerous-tool-use beta.
func TestPerCall_Safeguards(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	body := model.buildRequestBody(genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{
			"safeguards": []interface{}{
				map[string]interface{}{"type": "dangerous_tool_use"},
			},
		},
	}), false)

	sg, ok := body["safeguards"].([]map[string]interface{})
	if !ok || len(sg) != 1 || sg[0]["type"] != "dangerous_tool_use" {
		t.Fatalf("safeguards = %#v", body["safeguards"])
	}
	if h := model.combineBetaHeaders(genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{
			"safeguards": []interface{}{map[string]interface{}{"type": "dangerous_tool_use"}},
		},
	}), false); !strings.Contains(h, BetaHeaderDangerousToolUse) {
		t.Fatalf("anthropic-beta = %q, want to contain %q", h, BetaHeaderDangerousToolUse)
	}
}

// TestPerCall_SpeedServiceTierInferenceGeo verifies the three simple
// string-valued options flow per call.
func TestPerCall_SpeedServiceTierInferenceGeo(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeOpus4_6, nil)

	body := model.buildRequestBody(genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{
			"speed":        "fast",
			"serviceTier":  "standard_only",
			"inferenceGeo": "us",
		},
	}), false)

	if body["speed"] != "fast" {
		t.Errorf("speed = %v, want fast", body["speed"])
	}
	if body["service_tier"] != "standard_only" {
		t.Errorf("service_tier = %v, want standard_only", body["service_tier"])
	}
	if body["inference_geo"] != "us" {
		t.Errorf("inference_geo = %v, want us", body["inference_geo"])
	}
}

// TestPerCall_FallbacksArray verifies the per-call array form of
// providerOptions.anthropic.fallbacks flows to the request body and adds the
// server-side-fallback beta.
func TestPerCall_FallbacksArray(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	opts := genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{
			"fallbacks": []interface{}{
				map[string]interface{}{"model": "claude-haiku-4-5"},
			},
		},
	})

	req, err := model.prepareRequest(opts, false)
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	fbs, ok := req.body["fallbacks"].([]FallbackConfig)
	if !ok || len(fbs) != 1 || fbs[0].Model != "claude-haiku-4-5" {
		t.Fatalf("fallbacks = %#v", req.body["fallbacks"])
	}
	found := false
	for _, b := range req.betas {
		if b == BetaHeaderServerSideFallback {
			found = true
		}
	}
	if !found {
		t.Fatalf("betas = %v, want to contain %q", req.betas, BetaHeaderServerSideFallback)
	}
}

// TestPerCall_FallbacksDefault verifies the per-call literal
// providerOptions.anthropic.fallbacks = "default" flows to the request body
// and adds the server-side-fallback-default beta.
func TestPerCall_FallbacksDefault(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	opts := genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{"fallbacks": "default"},
	})

	req, err := model.prepareRequest(opts, false)
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	if req.body["fallbacks"] != "default" {
		t.Fatalf("fallbacks = %#v, want \"default\"", req.body["fallbacks"])
	}
	found := false
	for _, b := range req.betas {
		if b == BetaHeaderServerSideFallbackDefault {
			found = true
		}
	}
	if !found {
		t.Fatalf("betas = %v, want to contain %q", req.betas, BetaHeaderServerSideFallbackDefault)
	}
}

// TestPerCall_AnthropicBeta verifies providerOptions.anthropic.anthropicBeta
// flows per call, additively with betas the request otherwise computes.
func TestPerCall_AnthropicBeta(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	h := model.combineBetaHeaders(genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{"anthropicBeta": []interface{}{"custom-beta-2026-01-01"}},
	}), false)
	if !strings.Contains(h, "custom-beta-2026-01-01") {
		t.Fatalf("anthropic-beta = %q, want to contain custom-beta-2026-01-01", h)
	}
}

// TestPerCall_InvalidCanonicalProviderOptions ports the parseProviderOptions
// InvalidArgumentError shape: an invalid providerOptions.anthropic value is
// rejected before the request is built, with a message that names
// "anthropic" (TS: `invalid ${provider} provider options`).
func TestPerCall_InvalidCanonicalProviderOptions(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	opts := genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{
			"effort": "not-a-real-effort-level",
		},
	})

	_, err := model.prepareRequest(opts, false)
	if err == nil {
		t.Fatal("expected an error")
	}
	var invalidArg *providererrors.InvalidArgumentError
	if !errorsAs(err, &invalidArg) {
		t.Fatalf("error = %v, want *InvalidArgumentError", err)
	}
	if !strings.Contains(invalidArg.Message, "invalid anthropic provider options") {
		t.Fatalf("Message = %q, want to contain %q", invalidArg.Message, "invalid anthropic provider options")
	}
	if invalidArg.Field != "providerOptions" {
		t.Fatalf("Field = %q, want providerOptions", invalidArg.Field)
	}
}

// TestPerCall_ProviderOptionsNameSplitsOnFirstDot verifies
// LanguageModel.providerOptionsName mirrors TS getProviderOptionsName:
// the provider label up to (but not including) the first '.'.
func TestPerCall_ProviderOptionsNameSplitsOnFirstDot(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"anthropic", "anthropic"},
		{"minimax", "minimax"},
		{"bedrock.anthropic.messages", "bedrock"},
		{"anthropic-aws.messages", "anthropic-aws"},
	}
	for _, tt := range tests {
		prov := New(Config{APIKey: "test-key", Name: tt.name})
		model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)
		if got := model.providerOptionsName(); got != tt.want {
			t.Errorf("providerOptionsName() for Name=%q = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// errorsAs is a tiny local wrapper around errors.As to keep call sites
// terse across this file's many table-style assertions.
func errorsAs(err error, target **providererrors.InvalidArgumentError) bool {
	e, ok := err.(*providererrors.InvalidArgumentError)
	if !ok {
		return false
	}
	*target = e
	return true
}
