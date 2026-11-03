package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// A Responses function tool whose Parameters is a schema.Schema is sent as
// its JSON Schema and normalized like one (TS normalizeOpenAIJsonSchema runs
// on every tool.inputSchema): the lookaround pattern is removed with the
// compatibility warning, as for a map schema and as on Chat Completions.
func TestR84provbResponsesSchemaValueParametersNormalized(t *testing.T) {
	params := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"code": map[string]interface{}{"type": "string", "pattern": "^(?=a).*$"},
		},
	})
	model := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-5")
	body, _, warnings, err := model.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools:  []types.Tool{{Name: "lookup", Parameters: params}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(body["tools"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "pattern") || !strings.Contains(string(b), `"code":{"type":"string"}`) {
		t.Fatalf("tools = %s, want the lookaround pattern removed", b)
	}
	found := false
	for _, w := range warnings {
		if w.Feature == "JSON Schema pattern with regex lookaround" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %#v, want the lookaround compatibility warning", warnings)
	}
}
