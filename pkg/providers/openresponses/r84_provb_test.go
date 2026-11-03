package openresponses

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// The TS case suites of this package evaluate the extended assertion forms.
func init() { tsfixture.EnableExtendedAsserts() }

// TS sends parameters: tool.inputSchema for every function tool. A
// schema.Schema Parameters value was dropped (only maps and raw JSON were
// read), so the tool reached the wire without parameters.
func TestR84provbSchemaValueParametersSent(t *testing.T) {
	params := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
	})
	tools, _, _ := convertToolsToOpenResponses([]types.Tool{{Name: "weather", Parameters: params}}, nil, "")
	b, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"type":"function","name":"weather","parameters":{"properties":{"city":{"type":"string"}},"type":"object"}}]`
	if string(b) != want {
		t.Fatalf("tools =\n%s\nwant\n%s", b, want)
	}
}
