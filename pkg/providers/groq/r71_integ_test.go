package groq

import "testing"

// TS browserSearch inputSchema z.object({}) is not strict, and provider tool
// calls are validated against Parameters, so extra input keys must pass.
func TestR71IntegBrowserSearchInputSchemaNotStrict(t *testing.T) {
	params, _ := BrowserSearchTool().Parameters.(map[string]interface{})
	if _, ok := params["additionalProperties"]; ok || params["type"] != "object" {
		t.Errorf("Parameters = %#v, want a non-strict object schema", params)
	}
}
