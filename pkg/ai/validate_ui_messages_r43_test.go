package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS uiMessagesSchema: an input-streaming tool part's rawInput is
// z.string().optional(); other types fail validation. An output-error part's
// rawInput is z.unknown().
func TestValidateUIMessages_InputStreamingRawInputMustBeAString(t *testing.T) {
	msg := func(state string, rawInput interface{}) []interface{} {
		part := map[string]interface{}{"type": "tool-weather", "toolCallId": "c1", "state": state, "rawInput": rawInput}
		if state == "output-error" {
			part["errorText"] = "boom"
		}
		return []interface{}{map[string]interface{}{"id": "m1", "role": "assistant", "parts": []interface{}{part}}}
	}
	if _, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: msg("input-streaming", 5.0)}); err == nil {
		t.Fatal("input-streaming rawInput 5 was accepted")
	}
	if _, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: msg("input-streaming", "{\"a\"")}); err != nil {
		t.Fatalf("string rawInput: %v", err)
	}
	if _, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: msg("output-error", 5.0)}); err != nil {
		t.Fatalf("output-error rawInput: %v", err)
	}
}

// A document source's title is required (TS SourceDocument.title: string), so
// the UI chunk keeps an empty one and the resulting message validates.
func TestToUIMessageChunk_DocumentSourceKeepsEmptyTitle(t *testing.T) {
	sendSources := true
	chunk, ok := ToUIMessageChunk(provider.StreamChunk{
		Type:          provider.ChunkTypeSource,
		SourceContent: &types.SourceContent{SourceType: "document", ID: "s1", MediaType: "application/pdf"},
	}, UIMessageStreamResultOptions{SendSources: &sendSources})
	if !ok {
		t.Fatal("source chunk suppressed")
	}
	if title, present := chunk["title"]; !present || title != "" {
		t.Fatalf("chunk = %#v", chunk)
	}
	part := map[string]interface{}{"type": "source-document", "sourceId": "s1", "mediaType": "application/pdf", "title": chunk["title"]}
	msgs := []interface{}{map[string]interface{}{"id": "m", "role": "assistant", "parts": []interface{}{part}}}
	if _, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: msgs}); err != nil {
		t.Fatal(err)
	}
}
