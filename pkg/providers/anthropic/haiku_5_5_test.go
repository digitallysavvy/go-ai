package anthropic

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS #22239: claude-haiku-5-5 is a known adaptive-thinking model that
// rejects budget-based thinking but, unlike Sonnet 5.5, can disable thinking.
func TestHaiku5_5_Capabilities(t *testing.T) {
	caps := GetModelCapabilities(ClaudeHaiku5_5)
	want := ModelCapabilities{
		MaxOutputTokens: 128000, SupportsStructuredOutput: true, SupportsAdaptiveThinking: true,
		RejectsSamplingParameters: true, SupportsXHighEffort: true,
		RejectsThinkingDisabledAboveHighEffort: true, RejectsBudgetThinking: true, IsKnownModel: true,
	}
	if caps != want {
		t.Fatalf("caps = %+v, want %+v", caps, want)
	}
}

func TestHaiku5_5_BudgetThinkingBecomesAdaptive(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, ClaudeHaiku5_5, nil)
	req, err := m.prepareRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{
			"thinking": map[string]interface{}{"type": "enabled", "budgetTokens": 2000},
		}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	thinking, _ := req.body["thinking"].(map[string]interface{})
	if thinking["type"] != "adaptive" || thinking["budget_tokens"] != nil {
		t.Fatalf("thinking = %#v, want adaptive", thinking)
	}
	if len(req.warnings) != 1 || !strings.Contains(req.warnings[0].Details, "budget-based thinking is not supported by claude-haiku-5-5. Using adaptive thinking instead.") {
		t.Fatalf("warnings = %#v", req.warnings)
	}
}

func TestHaiku5_5_DisabledThinkingKeptAndEffortLowered(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, ClaudeHaiku5_5, nil)
	req, err := m.prepareRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{
			"thinking": map[string]interface{}{"type": "disabled"}, "effort": "max",
		}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	thinking, _ := req.body["thinking"].(map[string]interface{})
	oc, _ := req.body["output_config"].(map[string]interface{})
	if thinking["type"] != "disabled" || oc["effort"] != "high" {
		t.Fatalf("thinking=%#v output_config=%#v", thinking, oc)
	}
	if len(req.warnings) != 1 {
		t.Fatalf("warnings = %#v", req.warnings)
	}
}

func TestHaiku5_5_NoUnknownModelWarning(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, ClaudeHaiku5_5, nil)
	req, err := m.prepareRequest(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.warnings) != 0 || req.body["max_tokens"] != 128000 {
		t.Fatalf("warnings=%#v max_tokens=%v", req.warnings, req.body["max_tokens"])
	}
}
