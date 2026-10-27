package tools

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The error variant of every gateway search/fetch output schema is
// z.object({error: z.enum([...]), statusCode: z.number().optional(), message:
// z.string()}) in TS: error and message are required. Without `required`, the
// variant matched any object.
func TestSearchErrorVariantRequiresErrorAndMessage(t *testing.T) {
	tools := map[string]types.Tool{
		"exa":              NewExaSearch(ExaSearchConfig{}).ToTool(),
		"perplexity":       NewPerplexitySearch(PerplexitySearchConfig{}).ToTool(),
		"parallel":         NewParallelSearch(ParallelSearchConfig{}).ToTool(),
		"tako":             NewTakoSearch(TakoSearchConfig{}).ToTool(),
		"browserbase":      NewBrowserbaseSearch(BrowserbaseSearchConfig{}).ToTool(),
		"browserbaseFetch": NewBrowserbaseFetch(BrowserbaseFetchConfig{}).ToTool(),
	}
	for name, tool := range tools {
		t.Run(name, func(t *testing.T) {
			variants := tool.OutputSchema.(map[string]interface{})["oneOf"].([]interface{})
			var errVariant map[string]interface{}
			for _, v := range variants {
				m := v.(map[string]interface{})
				if props, ok := m["properties"].(map[string]interface{}); ok {
					if _, hasMessage := props["message"]; hasMessage {
						if _, hasError := props["error"]; hasError {
							errVariant = m
						}
					}
				}
			}
			if errVariant == nil {
				t.Fatal("no error variant")
			}
			if err := validateParallel(t, errVariant, `{"error":"timeout","message":"m","statusCode":504}`); err != nil {
				t.Errorf("valid error rejected: %v", err)
			}
			for _, bad := range []string{`{}`, `{"error":"timeout"}`, `{"message":"m"}`, `{"results":[]}`} {
				if err := validateParallel(t, errVariant, bad); err == nil {
					t.Errorf("%s accepted by the error variant", bad)
				}
			}
		})
	}
}
