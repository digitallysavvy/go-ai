package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestPrepareXAIResponsesTools_StrictForwarded verifies that a function
// tool's explicit Strict value (true or false) is forwarded to the wire,
// and omitted entirely when unset, matching TS
// xai-responses-prepare-tools.ts's `...(tool.strict != null ? {strict:
// tool.strict} : {})`.
func TestPrepareXAIResponsesTools_StrictForwarded(t *testing.T) {
	t.Parallel()

	tools := []types.Tool{
		{Name: "strict_tool", Parameters: map[string]interface{}{"type": "object"}, Strict: types.BoolPtr(true)},
		{Name: "non_strict_tool", Parameters: map[string]interface{}{"type": "object"}, Strict: types.BoolPtr(false)},
		{Name: "unspecified_tool", Parameters: map[string]interface{}{"type": "object"}},
	}

	result := prepareXAIResponsesTools(tools)
	if len(result) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(result))
	}

	m0, ok := result[0].(map[string]interface{})
	if !ok || m0["strict"] != true {
		t.Errorf("result[0] strict = %v, want true", m0["strict"])
	}

	m1, ok := result[1].(map[string]interface{})
	if !ok {
		t.Fatalf("result[1] is not a map: %#v", result[1])
	}
	if strictVal, present := m1["strict"]; !present || strictVal != false {
		t.Errorf("result[1] strict = %v (present=%v), want false", strictVal, present)
	}

	m2, ok := result[2].(map[string]interface{})
	if !ok {
		t.Fatalf("result[2] is not a map: %#v", result[2])
	}
	if _, present := m2["strict"]; present {
		t.Error("result[2] strict should be omitted when unset")
	}
}
