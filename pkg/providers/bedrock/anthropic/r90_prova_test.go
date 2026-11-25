package anthropic

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS 84208104a4 amazon-bedrock-anthropic-provider.test.ts: Bedrock disables
// native structured output and strict tools for the Fable 5.2 IDs.
func TestR90provaFable52ModelSupport(t *testing.T) {
	for _, id := range []string{"anthropic.claude-fable-5-2", "us.anthropic.claude-fable-5-2", "global.anthropic.claude-fable-5-2"} {
		if supportsNativeStructuredOutput(id) || supportsStrictTools(id) {
			t.Errorf("%s: native = %v, strict = %v, want false", id, supportsNativeStructuredOutput(id), supportsStrictTools(id))
		}
	}
}

// TS 84208104a4 amazon-bedrock-anthropic-structured-output.test.ts: "sends
// output_config.format for anthropic.claude-fable-5-2" (the model rejects
// forced tool use, so the JSON tool is not used).
func TestR90provaFable52StructuredOutput(t *testing.T) {
	body, res := r85provaGenerate(t, "anthropic.claude-fable-5-2", &provider.GenerateOptions{ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: r85provaSchema}})
	oc, _ := body["output_config"].(map[string]interface{})
	format, _ := oc["format"].(map[string]interface{})
	if format["type"] != "json_schema" || body["tools"] != nil || res.Text != `{"answer":"42"}` {
		t.Errorf("body = %v text = %q", body, res.Text)
	}
}
