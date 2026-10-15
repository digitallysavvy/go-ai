package prompt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A replayed assistant tool call keeps the key order of the input the model
// produced (TS passes the parsed input through unchanged). Go maps have no
// order, so the converters take it from the raw input text.
func TestAssistantToolInputKeepsRawKeyOrder(t *testing.T) {
	const raw = `{"zeta":1,"alpha":{"yy":1,"aa":2},"mid":[{"b":1,"a":2}]}`
	args := map[string]interface{}{
		"zeta": 1, "alpha": map[string]interface{}{"yy": 1, "aa": 2},
		"mid": []interface{}{map[string]interface{}{"b": 1, "a": 2}},
	}
	for _, withArgs := range []bool{true, false} {
		part := types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Input: raw}
		if withArgs {
			part.Arguments = args
		}
		msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{part}}}

		ab, err := json.Marshal(ToAnthropicMessages(msgs))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(ab), `"input":`+raw) {
			t.Errorf("anthropic (arguments=%v): input order lost: %s", withArgs, ab)
		}

		gb, err := json.Marshal(ToGoogleMessages(msgs, false))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(gb), `"args":`+raw) {
			t.Errorf("google (arguments=%v): args order lost: %s", withArgs, gb)
		}
	}
}

// When Arguments and the raw input diverge, the values come from Arguments;
// only the key order comes from the raw text.
func TestAssistantToolInputDivergedArgumentsWin(t *testing.T) {
	part := types.ToolCallContent{
		ToolCallID: "c1", ToolName: "t", Input: `{"b":1,"a":2}`,
		Arguments: map[string]interface{}{"a": 5, "b": 1, "c": 3},
	}
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{part}}}
	ab, _ := json.Marshal(ToAnthropicMessages(msgs)) //nolint:errcheck // test
	if !strings.Contains(string(ab), `"input":{"b":1,"a":5,"c":3}`) {
		t.Errorf("got %s", ab)
	}
}

func TestAssistantLegacyToolCallInputKeepsRawKeyOrder(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{
		ID: "c1", ToolName: "t", RawArguments: `{"z":1,"a":2}`, Arguments: map[string]interface{}{"z": 1, "a": 2},
	}}}}
	ab, _ := json.Marshal(ToAnthropicMessages(msgs)) //nolint:errcheck // test
	if !strings.Contains(string(ab), `"input":{"z":1,"a":2}`) {
		t.Errorf("got %s", ab)
	}
}
