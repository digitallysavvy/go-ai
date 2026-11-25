package anthropic

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS #23332: forced tool choice falls back to auto when manual
// (budget) thinking is enabled.
func forcedToolTools() []types.Tool {
	mk := func(n string) types.Tool {
		return types.Tool{Type: types.ToolTypeFunction, Name: n, Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}
	}
	return []types.Tool{mk("lookup"), mk("other")}
}

func toolNames(t *testing.T, body map[string]interface{}) []string {
	t.Helper()
	var out []string
	for _, tool := range body["tools"].([]map[string]interface{}) {
		out = append(out, tool["name"].(string))
	}
	return out
}

func TestForcedToolChoiceFallsBackToAutoWithManualThinking(t *testing.T) {
	choices := map[string]types.ToolChoice{
		"required": types.RequiredToolChoice(),
		"tool":     {Type: types.ToolChoiceTool, ToolName: "lookup"},
	}
	for name, choice := range choices {
		for _, source := range []string{"providerOptions", "reasoning"} {
			t.Run(name+"/"+source, func(t *testing.T) {
				m := NewLanguageModel(New(Config{APIKey: "k"}), "claude-haiku-4-5", nil)
				opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: forcedToolTools(), ToolChoice: choice}
				if source == "reasoning" {
					r := types.ReasoningLow
					opts.Reasoning = &r
				} else {
					opts.ProviderOptions = map[string]interface{}{"anthropic": map[string]interface{}{
						"thinking": map[string]interface{}{"type": "enabled", "budgetTokens": 5000},
					}}
				}
				req, err := m.prepareRequest(opts, false)
				if err != nil {
					t.Fatal(err)
				}
				if th, _ := req.body["thinking"].(map[string]interface{}); th["type"] != "enabled" {
					t.Fatalf("thinking = %#v", req.body["thinking"])
				}
				if tc, _ := req.body["tool_choice"].(map[string]interface{}); tc["type"] != "auto" {
					t.Fatalf("tool_choice = %#v, want auto", req.body["tool_choice"])
				}
				want := 2
				if name == "tool" {
					want = 1
				}
				if got := toolNames(t, req.body); len(got) != want {
					t.Fatalf("tools = %v", got)
				}
				found := false
				for _, w := range req.warnings {
					if w.Feature == "toolChoice" && strings.Contains(w.Details, "not supported with this model or thinking configuration") {
						found = true
					}
				}
				if !found {
					t.Fatalf("warnings = %#v", req.warnings)
				}
			})
		}
	}
}

func TestForcedToolChoicePreservedWithoutManualThinking(t *testing.T) {
	cases := []struct{ model, thinking, wantThinking string }{
		{"claude-haiku-4-5", "disabled", "disabled"},
		{"claude-sonnet-4-6", "adaptive", "adaptive"},
		// Budget thinking is normalized to adaptive on this model.
		{"claude-haiku-5-5", "enabled", "adaptive"},
	}
	for _, c := range cases {
		t.Run(c.model+"/"+c.thinking, func(t *testing.T) {
			m := NewLanguageModel(New(Config{APIKey: "k"}), c.model, nil)
			req, err := m.prepareRequest(&provider.GenerateOptions{
				Prompt: types.Prompt{Text: "hi"}, Tools: forcedToolTools(), ToolChoice: types.RequiredToolChoice(),
				ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{
					"thinking": map[string]interface{}{"type": c.thinking},
				}},
			}, false)
			if err != nil {
				t.Fatal(err)
			}
			if th, _ := req.body["thinking"].(map[string]interface{}); th["type"] != c.wantThinking {
				t.Fatalf("thinking = %#v", req.body["thinking"])
			}
			if tc, _ := req.body["tool_choice"].(map[string]interface{}); tc["type"] != "any" {
				t.Fatalf("tool_choice = %#v, want any", req.body["tool_choice"])
			}
			for _, w := range req.warnings {
				if w.Feature == "toolChoice" {
					t.Fatalf("unexpected toolChoice warning %#v", w)
				}
			}
		})
	}
}

func TestJSONResponseToolWithManualThinkingFallsBackToAuto(t *testing.T) {
	for _, c := range []struct{ model, mode string }{{"claude-sonnet-4-20250514", "auto"}, {"claude-sonnet-4-5", "jsonTool"}} {
		t.Run(c.model, func(t *testing.T) {
			m := NewLanguageModel(New(Config{APIKey: "k"}), c.model, nil)
			mt := 1000
			req, err := m.prepareRequest(&provider.GenerateOptions{
				Prompt: types.Prompt{Text: "hi"}, MaxTokens: &mt,
				ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
				ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{
					"structuredOutputMode": c.mode,
					"thinking":             map[string]interface{}{"type": "enabled", "budgetTokens": 5000},
				}},
			}, false)
			if err != nil {
				t.Fatal(err)
			}
			tc, _ := req.body["tool_choice"].(map[string]interface{})
			if tc["type"] != "auto" || tc["disable_parallel_tool_use"] != true {
				t.Fatalf("tool_choice = %#v", tc)
			}
			if names := toolNames(t, req.body); len(names) != 1 || names[0] != "json" {
				t.Fatalf("tools = %v", names)
			}
			if len(req.warnings) != 1 || req.warnings[0].Feature != "toolChoice" {
				t.Fatalf("warnings = %#v", req.warnings)
			}
		})
	}
}
