package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS amazon-bedrock-anthropic-model-support.test.ts (#22487).
func TestModelSupport_PreservesCapabilitiesTable(t *testing.T) {
	cases := []struct {
		id             string
		native, strict bool
	}{
		{"anthropic.claude-3-7-sonnet-20250219-v1:0", true, true},
		{"anthropic.claude-sonnet-4-20250514-v1:0", true, true},
		{"us.anthropic.claude-opus-4-1-20250805-v1:0", true, true},
		{"anthropic.claude-sonnet-4-5-20250929-v1:0", true, true},
		{"anthropic.claude-opus-4-6-v1", true, true},
		{"anthropic.claude-sonnet-4-6-v1", false, true},
		{"anthropic.claude-haiku-4-5-20251001-v1:0", false, true},
		{"anthropic.claude-opus-4-7", false, false},
		{"anthropic.claude-opus-5", false, false},
		{"anthropic.claude-sonnet-5-5", false, false},
		{"anthropic.claude-haiku-5-5", false, false},
		{"us.anthropic.claude-haiku-5-5", false, false},
		{"us.anthropic.claude-opus-6-v1:0", false, false},
		{"global.anthropic.claude-future-v1:0", false, false},
		{"anthropic.claude-opus-4-60-v1", false, false},
		{"meta.llama3-70b-instruct-v1:0", true, true},
		{"arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/custom", true, true},
	}
	for _, c := range cases {
		if got := bedrockSupportsNativeStructuredOutput(c.id); got != c.native {
			t.Errorf("bedrockSupportsNativeStructuredOutput(%q) = %v, want %v", c.id, got, c.native)
		}
		if got := bedrockSupportsStrictTools(c.id); got != c.strict {
			t.Errorf("bedrockSupportsStrictTools(%q) = %v, want %v", c.id, got, c.strict)
		}
	}
}

// Future Claude models fall back to JSON instructions for structured output
// instead of native output_config.format or a forced json tool.
func TestGetArgs_FutureClaudeUsesJSONInstructions(t *testing.T) {
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}}}
	for _, id := range []string{"us.anthropic.claude-opus-6-v1:0", "us.anthropic.claude-opus-4-9-v1:0"} {
		for _, withTools := range []bool{false, true} {
			p := New(Config{AWSAccessKeyID: "k", AWSSecretAccessKey: "s", Region: "us-east-1"})
			m := NewLanguageModel(p, id)
			opts := &provider.GenerateOptions{
				Prompt:         types.Prompt{Text: "Generate a name"},
				ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: schema},
			}
			if withTools {
				strict := true
				opts.Tools = []types.Tool{{Type: types.ToolTypeFunction, Name: "lookup", Strict: &strict,
					Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}}}
			}
			args, err := m.getArgs(opts)
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			if !args.UsesJSONInstruction || args.UsesJSONResponseTool {
				t.Errorf("%s tools=%v: UsesJSONInstruction=%v UsesJSONResponseTool=%v", id, withTools, args.UsesJSONInstruction, args.UsesJSONResponseTool)
			}
			if f, _ := args.Body["additionalModelRequestFields"].(map[string]interface{}); f != nil {
				if oc, _ := f["output_config"].(map[string]interface{}); oc["format"] != nil {
					t.Errorf("%s: output_config.format must be absent: %v", id, oc)
				}
			}
		}
	}
}
