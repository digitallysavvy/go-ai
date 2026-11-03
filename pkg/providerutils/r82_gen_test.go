package providerutils

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS toResponseMessages builds a text part as {type, text, providerOptions:
// part.providerMetadata}: the text content's citations are not part of the
// model message (generate-text.test.ts 9137 expects the response messages
// without them). Go copied them into the assistant message.
func TestR82genResponseTextPartHasNoCitations(t *testing.T) {
	start, end := 0, 5
	src := types.SourceContent{SourceType: "url", ID: "s1", URL: "https://example.com"}
	content := []types.ContentPart{types.TextContent{Text: "Hello", ProviderMetadata: json.RawMessage(`{"p":{"k":1}}`),
		Citations: []types.Citation{{Source: src, StartIndex: &start, EndIndex: &end}}}}
	msg := ConvertToResponseMessage(nil, content)
	if len(msg.Content) != 1 {
		t.Fatalf("content = %#v", msg.Content)
	}
	text, ok := msg.Content[0].(types.TextContent)
	if !ok || text.Text != "Hello" || text.Citations != nil || text.ProviderMetadata != nil || text.ProviderOptions["p"] == nil {
		t.Fatalf("text part = %#v", msg.Content[0])
	}
	if c := content[0].(types.TextContent); len(c.Citations) != 1 {
		t.Fatalf("source content part lost its citations: %#v", c)
	}
}
