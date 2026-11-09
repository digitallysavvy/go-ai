package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR89provaThinkingStripsUndeclaredKeys: the anthropicLanguageModelOptions
// thinking union strips the keys a thinking type does not declare (TS
// anthropic-language-model-options.ts, zod object strip): blockBinding is
// kept only for adaptive and the typeless binding-only form, display only
// for adaptive, and neither is validated for the other types. Expected
// values recorded from TS createAnthropic at 382d0b8eaf.
func TestR89provaThinkingStripsUndeclaredKeys(t *testing.T) {
	cases := []struct {
		name         string
		thinking     map[string]interface{}
		wantThinking string
		wantBeta     bool
	}{
		{
			name:         "enabled drops blockBinding",
			thinking:     map[string]interface{}{"type": "enabled", "budgetTokens": 2000, "blockBinding": map[string]interface{}{"prefixMismatchBehavior": "drop_block"}},
			wantThinking: `{"budget_tokens":2000,"type":"enabled"}`,
		},
		{
			name:         "enabled ignores an invalid display",
			thinking:     map[string]interface{}{"type": "enabled", "budgetTokens": 2000, "display": "bogus"},
			wantThinking: `{"budget_tokens":2000,"type":"enabled"}`,
		},
		{
			name:         "disabled ignores an invalid blockBinding",
			thinking:     map[string]interface{}{"type": "disabled", "blockBinding": map[string]interface{}{"prefixMismatchBehavior": "bogus"}},
			wantThinking: `{"type":"disabled"}`,
		},
		{
			name:         "adaptive keeps blockBinding",
			thinking:     map[string]interface{}{"type": "adaptive", "blockBinding": map[string]interface{}{"prefixMismatchBehavior": "drop_block"}},
			wantThinking: `{"block_binding":{"prefix_mismatch_behavior":"drop_block"},"type":"adaptive"}`,
			wantBeta:     true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := NewLanguageModel(New(Config{APIKey: "k"}), "claude-sonnet-4-5", nil)
			req, err := model.prepareRequest(&provider.GenerateOptions{
				Prompt:          types.Prompt{Text: "Hello"},
				ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thinking": tc.thinking}},
			}, false)
			if err != nil {
				t.Fatalf("prepareRequest: %v", err)
			}
			got, _ := json.Marshal(req.body["thinking"])
			if string(got) != tc.wantThinking {
				t.Fatalf("thinking = %s, want %s", got, tc.wantThinking)
			}
			hasBeta := strings.Contains(strings.Join(req.betas, ","), BetaHeaderThinkingBindingControls)
			if hasBeta != tc.wantBeta {
				t.Fatalf("betas = %v, want binding beta %v", req.betas, tc.wantBeta)
			}
		})
	}
}

// TestR89provaEmptyRequiredStrings: z.string() accepts an empty string, so
// an empty mcpServers name / url, anthropic skillId or fallback model is
// sent as is; only a missing key fails (TS anthropic-language-model-
// options.ts; recorded with TS createAnthropic at 382d0b8eaf).
func TestR89provaEmptyRequiredStrings(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "k"}), "claude-sonnet-4-5", nil)
	prepare := func(o map[string]interface{}) (*preparedRequest, error) {
		return model.prepareRequest(&provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "Hello"},
			ProviderOptions: map[string]interface{}{"anthropic": o},
		}, false)
	}
	req, err := prepare(map[string]interface{}{
		"mcpServers": []interface{}{map[string]interface{}{"type": "url", "name": "", "url": ""}},
		"container":  map[string]interface{}{"skills": []interface{}{map[string]interface{}{"type": "anthropic", "skillId": ""}}},
		"fallbacks":  []interface{}{map[string]interface{}{"model": ""}},
	})
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	got, _ := json.Marshal(map[string]interface{}{"m": req.body["mcp_servers"], "c": req.body["container"], "f": req.body["fallbacks"]})
	want := `{"c":{"skills":[{"skill_id":"","type":"anthropic"}]},"f":[{"model":""}],"m":[{"name":"","type":"url","url":""}]}`
	if string(got) != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
	if _, err := prepare(map[string]interface{}{"mcpServers": []interface{}{map[string]interface{}{"type": "url", "url": "u"}}}); err == nil {
		t.Fatal("a missing mcpServers name must fail")
	}
}
