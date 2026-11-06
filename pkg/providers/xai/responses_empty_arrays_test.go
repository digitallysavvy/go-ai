package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS forwards explicitly provided empty arrays (undefined is dropped by JSON,
// [] is not).
func TestPrepareXAIResponsesTools_ForwardsEmptyArrays(t *testing.T) {
	tools := []types.Tool{
		{Name: "web", Type: "provider", ProviderID: "xai.web_search", ProviderArgs: map[string]interface{}{"allowedDomains": []interface{}{}}},
		{Name: "fs", Type: "provider", ProviderID: "xai.file_search", ProviderArgs: map[string]interface{}{"vectorStoreIds": []interface{}{}}},
	}
	out, _ := prepareXAIResponsesToolsWithWarnings(tools)
	if v, ok := out[0].(map[string]interface{})["allowed_domains"]; !ok || len(v.([]string)) != 0 {
		t.Fatalf("allowed_domains not forwarded: %+v", out[0])
	}
	if _, ok := out[1].(map[string]interface{})["vector_store_ids"]; !ok {
		t.Fatalf("vector_store_ids not forwarded: %+v", out[1])
	}
}
