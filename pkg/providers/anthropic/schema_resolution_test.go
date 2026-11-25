package anthropic

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
	prov := New(Config{APIKey: "k"})
	s := schema.NewSimpleJSONSchema(testSchema)

	native := NewLanguageModel(prov, ClaudeSonnet4_6, nil).buildRequestBody(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "x"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: s},
	}, false)
	b, _ := json.Marshal(native["output_config"])
	if !strings.Contains(string(b), `"additionalProperties":false`) || !strings.Contains(string(b), `"name"`) {
		t.Fatalf("output_config.format.schema lost the schema: %s", b)
	}

	jsonTool := NewLanguageModel(prov, "claude-3-haiku-20240307", nil).buildRequestBody(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "x"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: s},
	}, false)
	b, _ = json.Marshal(jsonTool["tools"])
	if !strings.Contains(string(b), `"age"`) {
		t.Fatalf("json response tool lost the schema: %s", b)
	}

	fn := NewLanguageModel(prov, ClaudeSonnet4_6, nil).buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "x"},
		Tools:  []types.Tool{{Name: "t", Type: types.ToolTypeFunction, Parameters: s}},
	}, false)
	b, _ = json.Marshal(fn["tools"])
	if !strings.Contains(string(b), `"age"`) {
		t.Fatalf("function tool lost the schema: %s", b)
	}
}

// Raw JSON schemas keep the caller's property order on the wire (TS sanitizes
// a JS object, which keeps insertion order) and are sanitized.
func TestRawSchemaKeepsPropertyOrder(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	raw := json.RawMessage(`{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}}`)
	body := NewLanguageModel(prov, ClaudeSonnet4_6, nil).buildRequestBody(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "x"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: raw},
	}, false)
	b, _ := json.Marshal(body["output_config"])
	if !strings.Contains(string(b), `"properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}`) {
		t.Fatalf("property order lost: %s", b)
	}
	if !strings.Contains(string(b), `"additionalProperties":false`) {
		t.Fatalf("raw schema was not sanitized: %s", b)
	}
}
