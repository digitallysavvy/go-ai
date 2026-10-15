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

// TS: a whitespace-only tool description is omitted, and an empty (non-null)
// stopSequences list is still sent, or warned about for OpenAI models.
func TestBedrockEmptyToolDescriptionAndEmptyStopSequences(t *testing.T) {
	args, err := newBedrockModelWithID("us.anthropic.claude-sonnet-4-5-20250929-v1:0").getArgs(&provider.GenerateOptions{
		Prompt:        types.Prompt{Text: "hi"},
		StopSequences: []string{},
		Tools: []types.Tool{{Name: "t", Description: "   ", Type: types.ToolTypeFunction,
			Parameters: map[string]interface{}{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(args.Body)
	if strings.Contains(string(b), `"description"`) {
		t.Fatalf("whitespace description sent: %s", b)
	}
	if !strings.Contains(string(b), `"stopSequences":[]`) {
		t.Fatalf("empty stopSequences dropped: %s", b)
	}

	args, err = newBedrockModelWithID("openai.gpt-oss-120b-1:0").getArgs(&provider.GenerateOptions{
		Prompt:        types.Prompt{Text: "hi"},
		StopSequences: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range args.Warnings {
		found = found || w.Feature == "stopSequences"
	}
	if !found {
		t.Fatalf("no stopSequences warning: %v", args.Warnings)
	}
}

// A schema.OrderedSchema keeps its property order for the native schema, the
// instruction text and function tools.
func TestOrderedSchemaKeepsOrderOnTheWire(t *testing.T) {
	ordered, err := schema.NewOrderedSchema([]byte(`{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	const want = `"properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}`
	for name, modelID := range map[string]string{
		"native":      "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
		"instruction": "us.anthropic.claude-3-haiku-20240307-v1:0",
	} {
		args, err := newBedrockModelWithID(modelID).getArgs(&provider.GenerateOptions{
			Prompt:         types.Prompt{Text: "hi"},
			ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: ordered},
		})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(args.Body)
		if !strings.Contains(string(b), want) && !strings.Contains(strings.ReplaceAll(string(b), `\"`, `"`), want) {
			t.Errorf("%s: property order lost: %s", name, b)
		}
	}
	args, err := newBedrockModelWithID("us.anthropic.claude-sonnet-4-5-20250929-v1:0").getArgs(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools:  []types.Tool{{Name: "t", Type: types.ToolTypeFunction, Parameters: ordered}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(args.Body["toolConfig"])
	if !strings.Contains(string(b), want) {
		t.Errorf("function tool property order lost: %s", b)
	}
}
