package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// Hand ports of the anthropic TS assertions the replay harness does not
// evaluate and the evaluator will not cover (R87D_TSFIXTURE closure rule):
// CacheControlValidator state, a test's second call, Set.has, object
// identity (toBe) and input mutation. Each test runs the committed case's
// TS input and compares with the TS expected value.

var r87dprovrestPorted = map[string]string{}

// r87dprovrestNA are the assertions that cannot apply to Go.
var r87dprovrestNA = map[string]string{}

func r87dprovrestMark(goTest, test string, exprMatchers ...string) {
	for i := 0; i < len(exprMatchers); i += 2 {
		r87dprovrestPorted[tsfixture.PortedKey(test, exprMatchers[i], exprMatchers[i+1])] = goTest
	}
}

func init() {
	r87dprovrestMark("TestR87dprovrestAnthropicPrepareToolsNoneChoice", "should not affect auto and none tool choices",
		"none.tools", "toHaveLength", "none.toolChoice", "toEqual", "none.toolWarnings", "toEqual")
	for _, test := range []string{"should use usage as raw when rawUsage is not provided", "should sum across all iterations when iterations array is present"} {
		r87dprovrestNA[tsfixture.PortedKey(test, "result.raw", "toBe")] = "toBe is reference identity with the caller's usage object; convertAnthropicUsage takes the decoded wire struct, so Go cannot hand back the caller's object (TestR87dprovrestAnthropicConvertUsageRaw compares the value)"
	}
	r87dprovrestMark("TestR87dprovrestAnthropicSanitizeSchemaKeepsInput", "does not mutate the input schema", "schema", "toEqual")
	tsfixture.RegisterPorted(r87dprovrestPorted)
	tsfixture.RegisterNotApplicable(r87dprovrestNA)
}

// r87dprovrestValidatorRoot is the TS CacheControlValidator a test passes
// in, as the root the JS fallback calls (cacheControlValidator.getWarnings()).
func r87dprovrestValidatorRoot(v *prompt.AnthropicCacheControlValidator) map[string]interface{} {
	return map[string]interface{}{"getWarnings": tsfixture.Returns(r87dprovrestWarnings(v))}
}

// TS getWarnings() returns { type, feature, details } (no message).
func r87dprovrestWarnings(v *prompt.AnthropicCacheControlValidator) []interface{} {
	out := []interface{}{}
	for _, w := range v.Warnings() {
		out = append(out, map[string]interface{}{"type": w.Type, "feature": w.Feature, "details": w.Details})
	}
	return out
}

// r87dprovrestPrepareTools runs prepareTools on the first call's TS input of
// an anthropic-prepare-tools.test.ts case, with toolChoice replaced when set.
func r87dprovrestPrepareTools(t *testing.T, test string, toolChoice map[string]interface{}, v *prompt.AnthropicCacheControlValidator) preparedTools {
	t.Helper()
	tsfixture.ProviderToolHook = tsProviderTool
	defer func() { tsfixture.ProviderToolHook = nil }()
	args := r87dprovrestFnArgs(t, "testdata/tscases/prepare_tools.json.gz", test)
	in, _ := args[0].(map[string]interface{})
	o := prepareToolsOptions{
		supportsStructuredOutput: in["supportsStructuredOutput"] == true,
		supportsStrictTools:      in["supportsStrictTools"] == true,
		rejectsForcedToolUse:     in["rejectsForcedToolUse"] == true,
		validator:                v,
	}
	ts, why := tsfixture.BuildTools(in["tools"])
	if why != "" {
		t.Fatal(why)
	}
	o.tools = ts
	tc, _ := in["toolChoice"].(map[string]interface{})
	if toolChoice != nil {
		tc = toolChoice
	}
	if tc != nil {
		choice := tsfixture.BuildToolChoice(tc)
		o.toolChoice = &choice
	}
	out := prepareTools(o)
	if out.err != nil {
		t.Fatal(out.err)
	}
	return out
}

// anthropic-prepare-tools.test.ts 2196-2205: the test's second prepareTools
// call, with toolChoice { type: 'none' }.
func TestR87dprovrestAnthropicPrepareToolsNoneChoice(t *testing.T) {
	const test = "should not affect auto and none tool choices"
	none := r87dprovrestPrepareTools(t, test, map[string]interface{}{"type": "none"}, nil)
	if len(none.tools) != 2 {
		t.Errorf("none.tools = %d, TS 2", len(none.tools))
	}
	r87dprovrestEqual(t, "none.toolChoice", map[string]interface{}{"type": "none"}, none.toolChoice)
	r87dprovrestEqual(t, "none.toolWarnings", []interface{}{}, tsfixture.WarningsToTS(none.warnings))
}

// convert-anthropic-usage.test.ts 15 and 102: expect(result.raw).toBe(usage)
// (the same object; Go keeps the wire usage values as the raw usage).
func TestR87dprovrestAnthropicConvertUsageRaw(t *testing.T) {
	for _, test := range []string{"should use usage as raw when rawUsage is not provided", "should sum across all iterations when iterations array is present"} {
		args := r87dprovrestFnArgs(t, "testdata/tscases/convert_usage.json.gz", test)
		in, _ := args[0].(map[string]interface{})
		raw, _ := json.Marshal(in["usage"])
		var u anthropicUsage
		if err := json.Unmarshal(raw, &u); err != nil {
			t.Fatal(err)
		}
		r87dprovrestEqual(t, test, in["usage"], convertAnthropicUsage(u).Raw)
	}
}

// sanitize-json-schema.test.ts 156-177: sanitizeJsonSchema(schema) leaves
// schema as it was.
func TestR87dprovrestAnthropicSanitizeSchemaKeepsInput(t *testing.T) {
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "number", "exclusiveMinimum": 0}}}
	_ = tool.SanitizeAnthropicSchema(schema)
	r87dprovrestEqual(t, "schema", r87dprovrestArg(t, "testdata/tscases/sanitize_schema.json.gz", "does not mutate the input schema", 0, "schema"), schema)
}
