package openresponses

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR96provbMetadataJSONStringify: Open Responses provider metadata built
// from provider values (item ids, extension items) is written like
// JSON.stringify: <, > and & stay literal (main ruling 4, write side).
func TestR96provbMetadataJSONStringify(t *testing.T) {
	const v = "<a&b>"
	item := &OutputItem{Type: "reasoning", ID: v}
	var calls []types.ToolCall
	var content []types.ContentPart
	pushOpenResponsesCall(&calls, &content, types.ToolCall{ID: "c", ToolName: "t", ProviderMetadata: map[string]interface{}{"open-responses": map[string]interface{}{"itemId": v}}})
	carrier := buildExtensionReplayCarrier("ext", ExtensionRecord{"type": "x:item", "id": v}, "open-responses")
	for name, raw := range map[string][]byte{
		"reasoning": openResponsesReasoningProviderMetadata("open-responses", item),
		"text":      openResponsesTextMetadataPayload("open-responses", item, nil),
		"toolCall":  content[0].(types.ToolCallContent).ProviderMetadata,
		"carrier":   carrier.ProviderMetadata,
		"reference": mergeExtensionReference(nil, "open-responses", map[string]interface{}{"id": "ext", "itemId": v}),
	} {
		if !strings.Contains(string(raw), v) {
			t.Errorf("%s: %s, want %q written literally", name, raw, v)
		}
	}
}
