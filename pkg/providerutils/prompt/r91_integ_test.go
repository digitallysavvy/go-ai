package prompt

import (
	"reflect"
	"testing"
)

type r91integThinking struct {
	Type         string `json:"type"`
	BudgetTokens int64  `json:"budgetTokens,omitempty"`
}

// cr-r91-gen-to-integrator 2 (typed-option ruling, TS mergeObjects): a
// Go-typed namespace value (typed struct, map[string]string, pointer) is an
// object and merges with the other side instead of replacing it.
func TestR91integMergeProviderOptionsTypedObjects(t *testing.T) {
	base := map[string]interface{}{
		"anthropic": map[string]string{"effort": "low", "speed": "fast"},
		"openai":    &r91integThinking{Type: "enabled", BudgetTokens: 1024},
		"google":    map[string]interface{}{"list": []string{"a"}},
	}
	overrides := map[string]interface{}{
		"anthropic": map[string]interface{}{"effort": "high"},
		"openai":    r91integThinking{Type: "disabled"},
		"google":    map[string]interface{}{"list": []string{"b"}},
		"__proto__": map[string]interface{}{"x": 1},
	}
	got := MergeProviderOptions(base, overrides)
	want := map[string]interface{}{
		"anthropic": map[string]interface{}{"effort": "high", "speed": "fast"},
		"openai":    map[string]interface{}{"type": "disabled", "budgetTokens": float64(1024)},
		"google":    map[string]interface{}{"list": []string{"b"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %#v\nwant %#v", got, want)
	}
	if _, ok := base["anthropic"].(map[string]string); !ok {
		t.Fatal("base was mutated")
	}
}
