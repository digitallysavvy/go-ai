package google

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r57: TS compaction drops the results of a linked assistant message's
// tool-call parts; Go must do so for inline ToolCallContent parts too, not only
// msg.ToolCalls.
func TestR57InteractionsCompactionDropsInlineToolCallResults(t *testing.T) {
	meta := providerMetaRaw("", "v1_prev")
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "first"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.TextContent{Text: "old", ProviderMetadata: meta},
			types.ToolCallContent{ToolCallID: "call-old", ToolName: "lookup", Input: "{}"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.SimpleTextResult("call-old", "lookup", "old result")}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "next"}}},
	}
	out := compactMessagesForInteraction(msgs, "v1_prev")
	if len(out) != 2 || out[0].Role != types.RoleUser || out[1].Role != types.RoleUser {
		t.Fatalf("compacted messages = %#v, want only the two user messages", out)
	}
}

// r57: TS joins only summary items whose text is a string; a decoded item
// without text is skipped (a hand-built empty text is still joined).
func TestR57InteractionsThoughtTextSkipsItemsWithoutText(t *testing.T) {
	var summary []interactionsContentBlock
	if err := json.Unmarshal([]byte(`[{"type":"text","text":"a"},{"type":"text"},{"type":"text","text":null},{"type":"text","text":""},{"type":"text","text":"b"}]`), &summary); err != nil {
		t.Fatal(err)
	}
	if got := thoughtText(summary); got != "a\n\nb" {
		t.Fatalf("thoughtText = %q, want %q", got, "a\n\nb")
	}
}

// r57: TS filePartToImageBlock drops an image tool-result file with text data,
// with a warning.
func TestR57InteractionsToolResultImageWithTextDataDropped(t *testing.T) {
	for name, block := range map[string]types.FileContentBlock{
		"fileData": {MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeText, Text: "x"}},
		"legacy":   {MediaType: "image/png", Text: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			out, warnings, err := toolResultContentBlocks([]types.ToolResultContentBlock{block})
			if err != nil {
				t.Fatal(err)
			}
			if len(out) != 0 {
				t.Fatalf("blocks = %#v, want none", out)
			}
			want := "google.interactions: tool-result image part with `data.type === \"text\"` is not representable as an image; part dropped."
			if len(warnings) != 1 || warnings[0].Message != want {
				t.Fatalf("warnings = %#v", warnings)
			}
		})
	}
}

// r57 integrator: a summary item whose text is not a string (number, object)
// fails the strict decode; the loose decode leaves Text "" and marks it absent,
// so it is not joined (TS typeof item.text === 'string').
func TestR57InteractionsThoughtTextSkipsNonStringTextLoose(t *testing.T) {
	var summary []interactionsContentBlock
	looseDecode([]byte(`[{"type":"text","text":"a"},{"type":"text","text":5},{"type":"text","text":{"x":1}},{"type":"text","text":"b"}]`), &summary)
	if len(summary) != 4 {
		t.Fatalf("summary = %d items, want 4", len(summary))
	}
	if got := thoughtText(summary); got != "a\nb" {
		t.Fatalf("thoughtText = %q, want %q", got, "a\nb")
	}
}
