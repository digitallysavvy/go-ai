package ai

import (
	"context"
	"encoding/json"
	"testing"
)

// r89streamEmptyCitationMessages is a UI message whose text part carries a
// url citation and a document citation with empty strings in every required
// string field. TS citationSchema (types/citation.ts) types them as
// z.string(), which accepts "": node with
// packages/ai/dist validateUIMessages returns the messages unchanged.
const r89streamEmptyCitationMessages = `[{"id":"1","role":"assistant","parts":[{"type":"text","text":"hi","citations":[` +
	`{"source":{"type":"source","sourceType":"url","id":"","url":""}},` +
	`{"source":{"type":"source","sourceType":"document","id":"","mediaType":"","title":""}}]}]}]`

// TS validateUIMessages accepts citations whose id, url, mediaType or title
// is "" (citationSchema uses z.string(), not z.string().min(1)).
func TestR89streamValidateUIMessagesAcceptsEmptyCitationStrings(t *testing.T) {
	messages, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: json.RawMessage(r89streamEmptyCitationMessages)})
	if err != nil {
		t.Fatalf("ValidateUIMessages: %v (TS accepts empty citation strings)", err)
	}
	text, ok := messages[0].Parts[0].(*TextUIPart)
	if !ok || len(text.Citations) != 2 {
		t.Fatalf("parts[0] = %#v, want a text part with 2 citations", messages[0].Parts[0])
	}
	if got := text.Citations[1].Source.SourceType; got != "document" {
		t.Errorf("citations[1].source.sourceType = %q, want document", got)
	}
}

// A UI message decodes like TS JSON.parse: empty citation strings are kept.
func TestR89streamUIMessageDecodesEmptyCitationStrings(t *testing.T) {
	var messages []UIMessage
	if err := json.Unmarshal([]byte(r89streamEmptyCitationMessages), &messages); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	snapshot, err := UIMessageFromChunk(UIMessageChunk{"id": "1", "role": "assistant", "parts": []interface{}{
		map[string]interface{}{"type": "text", "text": "hi", "citations": []interface{}{
			map[string]interface{}{"source": map[string]interface{}{"type": "source", "sourceType": "document", "id": "", "mediaType": "", "title": ""}},
		}},
	}})
	if err != nil {
		t.Fatalf("UIMessageFromChunk: %v", err)
	}
	if text, ok := snapshot.Parts[0].(*TextUIPart); !ok || len(text.Citations) != 1 {
		t.Fatalf("snapshot parts[0] = %#v, want a text part with 1 citation", snapshot.Parts[0])
	}
}
