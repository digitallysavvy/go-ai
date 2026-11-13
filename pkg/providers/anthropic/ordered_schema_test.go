package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// A schema.OrderedSchema (what a caller builds to keep key order, like a TS
// JS object) keeps its property order on the wire for the response schema and
// for function tools.
func TestOrderedSchemaKeepsOrderOnTheWire(t *testing.T) {
	ordered, err := schema.NewOrderedSchema([]byte(`{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	prov := New(Config{APIKey: "k"})
	want := `"properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}`

	native := NewLanguageModel(prov, ClaudeSonnet4_6, nil).buildRequestBody(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "x"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: ordered},
	}, false)
	b, _ := json.Marshal(native["output_config"])
	if !strings.Contains(string(b), want) {
		t.Errorf("output_config schema order lost: %s", b)
	}

	jsonTool := NewLanguageModel(prov, "claude-3-haiku-20240307", nil).buildRequestBody(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "x"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: ordered},
	}, false)
	b, _ = json.Marshal(jsonTool["tools"])
	if !strings.Contains(string(b), want) {
		t.Errorf("json response tool schema order lost: %s", b)
	}

	fn := NewLanguageModel(prov, ClaudeSonnet4_6, nil).buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "x"},
		Tools:  []types.Tool{{Name: "t", Type: types.ToolTypeFunction, Parameters: ordered}},
	}, false)
	b, _ = json.Marshal(fn["tools"])
	if !strings.Contains(string(b), want) {
		t.Errorf("function tool schema order lost: %s", b)
	}
}
