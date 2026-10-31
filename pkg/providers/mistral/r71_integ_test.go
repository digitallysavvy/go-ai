package mistral

import "testing"

// TS web search inputSchema z.object({ arguments: z.string() }) is not
// strict, and provider tool calls are validated against Parameters.
func TestR71IntegWebSearchInputSchemaNotStrict(t *testing.T) {
	for _, tool := range []interface{}{WebSearch().Parameters, WebSearchPremium().Parameters} {
		params, _ := tool.(map[string]interface{})
		if _, ok := params["additionalProperties"]; ok || params["type"] != "object" {
			t.Errorf("Parameters = %#v, want a non-strict object schema", params)
		}
	}
}
