package google

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r60: TS safeParseToolArgs uses secureJsonParse, so a stringified tool-call
// input carrying a prototype-polluting key is sent as {value: input}
// (convert-to-google-interactions-input.test.ts "rejects prototype-polluting
// stringified tool-call input").
func TestR60InteractionsToolCallInputRejectsPrototypeKeys(t *testing.T) {
	m := &InteractionsLanguageModel{}
	for _, input := range []string{
		`{"__proto__":{"polluted":true}}`,
		`{"a":{"constructor":{"prototype":{"x":1}}}}`,
		`[{"__proto__":1}]`,
	} {
		steps, _, err := m.convertAssistantSteps([]types.ContentPart{
			types.ToolCallContent{ToolCallID: "call_abc", ToolName: "getWeather", Input: input},
		}, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		args, _ := steps[0]["arguments"].(map[string]interface{})
		if len(args) != 1 || args["value"] != input {
			t.Errorf("input %s: arguments = %#v, want {value: input}", input, steps[0]["arguments"])
		}
	}
	steps, _, _ := m.convertAssistantSteps([]types.ContentPart{
		types.ToolCallContent{ToolCallID: "c", ToolName: "t", Input: `{"constructor":"x","location":"Boston"}`},
	}, nil, "")
	if b, _ := json.Marshal(steps[0]["arguments"]); string(b) != `{"constructor":"x","location":"Boston"}` {
		t.Errorf("arguments = %s, want the parsed object", b)
	}
}
