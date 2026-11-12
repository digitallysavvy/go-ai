package bedrock

import (
	"encoding/json"
	"reflect"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR94provaReasoningMetadataValidated: TS reads reasoning metadata with
// parseProviderOptions(amazonBedrockReasoningMetadataSchema) on amazonBedrock
// then bedrock, so a non-object namespace or a non-string field is an
// InvalidArgumentError (not skipped, and no fallback to the legacy key).
func TestR94provaReasoningMetadataValidated(t *testing.T) {
	for name, opts := range map[string]map[string]interface{}{
		"non-string signature": {"amazonBedrock": map[string]interface{}{"signature": 5}},
		"non-object namespace": {"amazonBedrock": "x", "bedrock": map[string]interface{}{"signature": "s"}},
		"non-string redacted":  {"bedrock": map[string]interface{}{"redactedContent": true}},
	} {
		msgs := []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ReasoningContent{Text: "r", ProviderOptions: opts},
				types.TextContent{Text: "a"},
			}},
		}
		_, _, err := convertToBedrockChatMessages(msgs, false)
		if !providererrors.IsInvalidArgumentError(err) {
			t.Errorf("%s: err = %T %v, want InvalidArgumentError", name, err, err)
		}
	}

	// An explicit null field is absent (known difference) and a valid
	// signature is replayed.
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ReasoningContent{Text: "r", ProviderOptions: map[string]interface{}{"amazonBedrock": map[string]interface{}{"signature": "sig", "redactedData": nil}}},
		}},
	}
	_, out, err := convertToBedrockChatMessages(msgs, false)
	if err != nil {
		t.Fatal(err)
	}
	content := out[1]["content"].([]map[string]interface{})
	rc := content[0]["reasoningContent"].(map[string]interface{})["reasoningText"].(map[string]interface{})
	if rc["signature"] != "sig" {
		t.Fatalf("content = %v", content)
	}
}

// TestR94provaRerankWireItemTags (cr-r94-carry-to-prova): the rerank result
// decodes into a wire item whose json tags are the exact TS schema keys
// (types.RerankItem is untagged, which only case-insensitive matching filled).
func TestR94provaRerankWireItemTags(t *testing.T) {
	typ := reflect.TypeOf(bedrockRerankWireItem{})
	want := map[string]string{"Index": "index", "RelevanceScore": "relevanceScore"}
	if typ.NumField() != len(want) {
		t.Fatalf("fields = %d", typ.NumField())
	}
	for name, tag := range want {
		f, ok := typ.FieldByName(name)
		if !ok || f.Tag.Get("json") != tag {
			t.Errorf("%s tag = %q, want %q", name, f.Tag.Get("json"), tag)
		}
	}
	var parsed struct {
		Results []bedrockRerankWireItem `json:"results"`
	}
	if err := json.Unmarshal([]byte(`{"results":[{"index":2,"relevanceScore":0.5}]}`), &parsed); err != nil || len(parsed.Results) != 1 || parsed.Results[0] != (bedrockRerankWireItem{Index: 2, RelevanceScore: 0.5}) {
		t.Fatalf("parsed = %+v, %v", parsed, err)
	}
}
