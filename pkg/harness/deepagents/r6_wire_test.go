package deepagents

import (
	"encoding/json"
	"testing"
)

// TS `thinkingSchema`: "enabled" requires budget_tokens, "disabled" has no
// other fields.
func TestThinkingConfig_WireShapes(t *testing.T) {
	for _, tc := range []struct {
		in   ThinkingConfig
		want string
	}{
		{ThinkingConfig{Type: ThinkingTypeEnabled}, `{"budget_tokens":0,"type":"enabled"}`},
		{ThinkingConfig{Type: ThinkingTypeEnabled, BudgetTokens: 2048, Display: "omitted"}, `{"budget_tokens":2048,"display":"omitted","type":"enabled"}`},
		{ThinkingConfig{Type: ThinkingTypeAdaptive, Display: "summarized", BudgetTokens: 5}, `{"display":"summarized","type":"adaptive"}`},
		{ThinkingConfig{Type: ThinkingTypeDisabled, Display: "omitted"}, `{"type":"disabled"}`},
	} {
		b, err := json.Marshal(tc.in)
		if err != nil || string(b) != tc.want {
			t.Errorf("%+v = %s, %v; want %s", tc.in, b, err, tc.want)
		}
	}
}
