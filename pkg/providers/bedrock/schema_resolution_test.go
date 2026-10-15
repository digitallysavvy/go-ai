package bedrock

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// ai.GenerateObject passes a schema.Schema as ResponseFormat.Schema and Go
// callers pass one as Tool.Parameters; neither may reach the wire as {}.
func TestSchemaSchemaReachesTheWire(t *testing.T) {
	s := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"age": map[string]interface{}{"type": "integer"}},
	})
	for name, modelID := range map[string]string{
		"native":      "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
		"instruction": "us.anthropic.claude-3-haiku-20240307-v1:0",
	} {
		t.Run(name, func(t *testing.T) {
			args, err := newBedrockModelWithID(modelID).getArgs(&provider.GenerateOptions{
				Prompt:         types.Prompt{Text: "hi"},
				ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: s},
			})
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(args.Body)
			if !strings.Contains(string(b), `age`) {
				t.Fatalf("schema lost: %s", b)
			}
		})
	}

	args, err := newBedrockModelWithID("us.anthropic.claude-sonnet-4-5-20250929-v1:0").getArgs(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools:  []types.Tool{{Name: "t", Type: types.ToolTypeFunction, Parameters: s}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(args.Body["toolConfig"])
	if !strings.Contains(string(b), `age`) {
		t.Fatalf("function tool schema lost: %s", b)
	}
}
