package perplexity

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS countImages reads the JSON object form of the state, so an image_url
// object whose image_url is a typed Go map (same JSON) is a valid image, and
// one with a non-data URL is still rejected.
func TestR83provaDecisionStateTypedImageURLObject(t *testing.T) {
	ok := map[string]interface{}{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64,AQID"}}
	got, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{provider.DecisionJSONPart(ok)})
	if err != nil {
		t.Fatalf("typed image_url map: %v", err)
	}
	if m, _ := got.(map[string]interface{}); m["type"] != "image_url" {
		t.Fatalf("state = %#v, want the object as native state", got)
	}
	type imageURL struct {
		URL string `json:"url"`
	}
	bad := map[string]interface{}{"type": "image_url", "image_url": imageURL{URL: "https://example.com/a.png"}}
	var unsupported *providererrors.UnsupportedFunctionalityError
	if _, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{provider.DecisionJSONPart(bad)}); !errors.As(err, &unsupported) {
		t.Fatalf("non-data image URL in a struct: err = %v, want UnsupportedFunctionalityError", err)
	}
	nine := make([]interface{}, 9)
	for i := range nine {
		nine[i] = ok
	}
	var invalid *providererrors.InvalidArgumentError
	if _, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{provider.DecisionJSONPart(nine)}); !errors.As(err, &invalid) {
		t.Fatalf("9 typed images: err = %v, want InvalidArgumentError", err)
	}
}

// TS getThoughtSignature forwards any string, "" included
// (`typeof thoughtSignature === 'string'`), on function_call and
// function_call_output items.
func TestR83provaInputEmptyThoughtSignatureForwarded(t *testing.T) {
	opts := map[string]interface{}{"perplexity": map[string]interface{}{"thoughtSignature": ""}}
	msgs := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "c1", ToolName: "f", Input: "{}", ProviderOptions: opts},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "c1", ToolName: "f", Result: "ok", ProviderOptions: opts},
		}},
	}
	input, _, err := convertToPerplexityInput(msgs)
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range input {
		if sig, ok := item["thought_signature"]; !ok || sig != "" {
			t.Errorf("input[%d].thought_signature = %v (present %v), TS sends \"\"", i, sig, ok)
		}
	}
}
