package bedrock

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS 84208104a4 amazon-bedrock-prepare-tools.test.ts: "should warn when strict
// is omitted for %s" covers the Fable 5.2 IDs.
func TestR90provaFable52StrictWarning(t *testing.T) {
	strict := true
	tools := []types.Tool{{Type: types.ToolTypeFunction, Name: "testFunction", Description: "A test function", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}, Strict: &strict}}
	for _, id := range []string{ModelAnthropicClaudeFable5_2, ModelUSAnthropicClaudeFable5_2, ModelGlobalAnthropicClaudeFable5_2} {
		res := prepareBedrockTools(tools, types.ToolChoice{}, false, id, "", nil, nil, false)
		spec, _ := res.ToolConfig.Tools[0]["toolSpec"].(map[string]interface{})
		if _, ok := spec["strict"]; ok {
			t.Errorf("%s: toolSpec.strict present", id)
		}
		want := "Tool 'testFunction' has strict: true, but strict mode is not supported by this model on Amazon Bedrock. The strict property will be ignored."
		if len(res.Warnings) != 1 || res.Warnings[0].Type != "unsupported" || res.Warnings[0].Feature != "strict" || res.Warnings[0].Details != want {
			t.Errorf("%s: warnings = %#v", id, res.Warnings)
		}
	}
}

// TS amazon-bedrock-chat-language-model.ts 906 / 916: the json response tool
// text is JSON.stringify(toolUse.input) and a tool call input
// JSON.stringify(toolUse.input ?? {}), so a null input is "null" / "{}" and a
// non-object input keeps its JSON text (TS recorded at 9f7a842ae1).
func TestR90provaConverseNonObjectToolInput(t *testing.T) {
	m := &LanguageModel{modelID: "us.anthropic.claude-opus-4-7"}
	res, err := m.convertConverseResponse([]byte(`{"output":{"message":{"role":"assistant","content":[{"toolUse":{"toolUseId":"j","name":"json","input":null}}]}},"stopReason":"tool_use","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}`), false, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "null" {
		t.Errorf("json tool text = %q, want null", res.Text)
	}
	res, err = m.convertConverseResponse([]byte(`{"output":{"message":{"role":"assistant","content":[{"toolUse":{"toolUseId":"a","name":"t","input":null}},{"toolUse":{"toolUseId":"b","name":"t","input":[1]}},{"toolUse":{"toolUseId":"c","name":"t","input":"s"}},{"toolUse":{"toolUseId":"d","name":"t","input":{"z":1,"a":2}}}]}},"stopReason":"tool_use","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}`), false, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tc := range res.ToolCalls {
		got = append(got, tc.RawArguments)
	}
	if want := `{}|[1]|"s"|{"z":1,"a":2}`; strings.Join(got, "|") != want {
		t.Errorf("inputs = %s, want %s", strings.Join(got, "|"), want)
	}
}

// TS amazon-bedrock-chat-language-model.ts 257 / 268: `${temperature}` is a
// JS number, not Go's %v exponent form.
func TestR90provaTemperatureWarningNumberFormat(t *testing.T) {
	for temp, want := range map[float64]string{
		1234567:  "1234567 exceeds bedrock maximum of 1.0. clamped to 1.0",
		-0.00001: "-0.00001 is below bedrock minimum of 0. clamped to 0",
	} {
		temp := temp
		args := r85provaArgs(t, "anthropic.claude-3-haiku-20240307-v1:0", &provider.GenerateOptions{Temperature: &temp})
		found := false
		for _, w := range args.Warnings {
			found = found || w.Details == want
		}
		if !found {
			t.Errorf("warnings = %#v, want %q", args.Warnings, want)
		}
	}
}
