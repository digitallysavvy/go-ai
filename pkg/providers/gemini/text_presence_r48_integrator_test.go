package gemini

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A thought part with a thoughtSignature but no text key is not a text part
// in TS (`'text' in part && part.text != null`), so its signature is not
// attached to the previous content; an empty thought text is.
func TestR48_GenerateThoughtSignatureOnlyPartDoesNotAttach(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	for body, wantMeta := range map[string]bool{
		`{"candidates":[{"content":{"parts":[{"text":"a"},{"thought":true,"thoughtSignature":"s"}]},"finishReason":"STOP"}]}`:             false,
		`{"candidates":[{"content":{"parts":[{"text":"a"},{"thought":true,"text":null,"thoughtSignature":"s"}]},"finishReason":"STOP"}]}`: false,
		`{"candidates":[{"content":{"parts":[{"text":"a"},{"thought":true,"text":"","thoughtSignature":"s"}]},"finishReason":"STOP"}]}`:   true,
	} {
		var resp Response
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatal(err)
		}
		result := m.convertResponse(resp, nil)
		text := result.Content[0].(types.TextContent)
		if got := len(text.ProviderMetadata) > 0; got != wantMeta {
			t.Errorf("%s: metadata attached = %v, want %v", body, got, wantMeta)
		}
	}
}
