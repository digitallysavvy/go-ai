package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func ptrLevel(l types.ReasoningLevel) *types.ReasoningLevel { return &l }

// TS #22045: budget-based reasoning maps max to 95% of maxOutputTokens.
func TestBedrockReasoningMaxBudget(t *testing.T) {
	var w []types.Warning
	// Sonnet 4.5: maxOutputTokens=64000, 95% = 60800.
	rc := resolveBedrockReasoningConfig(ptrLevel(types.ReasoningMax), nil, true, false, "anthropic.claude-sonnet-4-5-20250929-v1:0", &w)
	if rc == nil || rc.Type != "enabled" || rc.BudgetTokens == nil || *rc.BudgetTokens != 60800 {
		t.Fatalf("rc = %#v", rc)
	}
}

// TS #22045: adaptive Claude models send native max effort without a warning.
func TestBedrockReasoningMaxAdaptiveEffort(t *testing.T) {
	var w []types.Warning
	rc := resolveBedrockReasoningConfig(ptrLevel(types.ReasoningMax), nil, true, false, "anthropic.claude-opus-5", &w)
	if rc == nil || rc.Type != "adaptive" || rc.MaxReasoningEffort != "max" {
		t.Fatalf("rc = %#v", rc)
	}
	if len(w) != 0 {
		t.Fatalf("warnings = %#v", w)
	}
}

// TS #22045: Nova 2 maps xhigh and max to high with a compatibility warning.
func TestBedrockReasoningNovaMaxWarns(t *testing.T) {
	for _, lvl := range []types.ReasoningLevel{types.ReasoningXHigh, types.ReasoningMax} {
		var w []types.Warning
		rc := resolveBedrockReasoningConfig(ptrLevel(lvl), nil, false, false, ModelAmazonNova2LiteV1, &w)
		if rc == nil || rc.Type != "enabled" || rc.MaxReasoningEffort != "high" {
			t.Fatalf("%s: rc = %#v", lvl, rc)
		}
		if len(w) != 1 || w[0].Type != "compatibility" {
			t.Fatalf("%s: warnings = %#v", lvl, w)
		}
	}
}

// TS #22045: OpenAI models on Bedrock send max natively.
func TestBedrockReasoningOpenAIMax(t *testing.T) {
	var w []types.Warning
	rc := resolveBedrockReasoningConfig(ptrLevel(types.ReasoningMax), nil, false, true, "openai.gpt-oss-120b-1:0", &w)
	if rc == nil || rc.MaxReasoningEffort != "max" || len(w) != 0 {
		t.Fatalf("rc = %#v w = %#v", rc, w)
	}
}

// TS #22045: partial provider-option configs are preserved field by field,
// and an explicitly provided field is never mapped (no warning).
func TestBedrockReasoningPartialConfigPreserved(t *testing.T) {
	var w []types.Warning
	existing := &ReasoningConfig{MaxReasoningEffort: "low"}
	rc := resolveBedrockReasoningConfig(ptrLevel(types.ReasoningMax), existing, false, false, ModelAmazonNova2LiteV1, &w)
	if rc.MaxReasoningEffort != "low" || rc.Type != "enabled" || len(w) != 0 {
		t.Fatalf("rc = %#v w = %#v", rc, w)
	}

	// Adaptive Claude: provider option type wins, derived effort fills in.
	existing = &ReasoningConfig{Type: "adaptive"}
	rc = resolveBedrockReasoningConfig(ptrLevel(types.ReasoningHigh), existing, true, false, "anthropic.claude-opus-5", &w)
	if rc.Type != "adaptive" || rc.MaxReasoningEffort != "high" {
		t.Fatalf("rc = %#v", rc)
	}

	// Explicit budget wins over the derived one.
	budget := 2000
	existing = &ReasoningConfig{BudgetTokens: &budget}
	rc = resolveBedrockReasoningConfig(ptrLevel(types.ReasoningMax), existing, true, false, "anthropic.claude-sonnet-4-5-20250929-v1:0", &w)
	if rc.Type != "enabled" || rc.BudgetTokens == nil || *rc.BudgetTokens != 2000 {
		t.Fatalf("rc = %#v", rc)
	}
}

// TS #22045: a disabled provider-option config is preserved without deriving
// effort/budget or warnings.
func TestBedrockReasoningDisabledConfigPreserved(t *testing.T) {
	var w []types.Warning
	existing := &ReasoningConfig{Type: "disabled", MaxReasoningEffort: "high"}
	rc := resolveBedrockReasoningConfig(ptrLevel(types.ReasoningMax), existing, true, false, "anthropic.claude-opus-5", &w)
	if rc.Type != "disabled" || rc.MaxReasoningEffort != "" || rc.BudgetTokens != nil || len(w) != 0 {
		t.Fatalf("rc = %#v w = %#v", rc, w)
	}
	if existing.MaxReasoningEffort != "high" {
		t.Fatal("existing config was mutated")
	}
}
