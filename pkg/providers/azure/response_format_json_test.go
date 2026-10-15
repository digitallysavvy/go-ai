package azure

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Core now sends responseFormat type "json" (TS); "json" is not a wire value,
// so Azure maps it to json_schema / json_object.
func TestBuildRequestBody_JSONResponseFormatMapsToWireTypes(t *testing.T) {
	m := NewLanguageModel(&Provider{}, "d")
	prompt := types.Prompt{Text: "x"}

	body := m.buildRequestBody(&provider.GenerateOptions{Prompt: prompt, ResponseFormat: &provider.ResponseFormat{Type: "json"}}, false)
	if rf, _ := body["response_format"].(map[string]interface{}); rf["type"] != "json_object" {
		t.Fatalf("no schema: response_format = %#v, want json_object", body["response_format"])
	}

	body = m.buildRequestBody(&provider.GenerateOptions{Prompt: prompt, ResponseFormat: &provider.ResponseFormat{
		Type: "json", Schema: map[string]interface{}{"type": "object"},
	}}, false)
	rf, _ := body["response_format"].(map[string]interface{})
	if rf["type"] != "json_schema" || rf["json_schema"] == nil {
		t.Fatalf("schema: response_format = %#v, want json_schema", body["response_format"])
	}
}
