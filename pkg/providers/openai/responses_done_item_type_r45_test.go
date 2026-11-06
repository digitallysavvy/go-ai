package openai

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS dispatches response.output_item.done on the done item's own type: a
// function_call done item with no recorded output_item.added still yields
// tool-input-end and tool-call, and a done type that differs from the added
// type follows the done type.
func TestResponsesStreamDoneEventDispatchesOnDoneItemType(t *testing.T) {
	cases := map[string]string{
		"no added event": `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","status":"completed","id":"fc_1","call_id":"call_1","name":"weather","arguments":"{\"a\":1}"}}` + "\n\n",
		"added message, done function_call": `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","status":"in_progress","role":"assistant","content":[]}}` + "\n\n" +
			`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","status":"completed","id":"fc_1","call_id":"call_1","name":"weather","arguments":"{\"a\":1}"}}` + "\n\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			s := newResponsesStream(io.NopCloser(strings.NewReader(body)), false)
			var call *provider.StreamChunk
			for {
				c, err := s.Next()
				if err != nil {
					break
				}
				if c.Type == provider.ChunkTypeToolCall {
					call = c
				}
			}
			if call == nil || call.ToolCall.ID != "call_1" || call.ToolCall.ToolName != "weather" || call.ToolCall.RawArguments != `{"a":1}` {
				t.Fatalf("tool call = %+v", call)
			}
		})
	}
}

// A reasoning item with an empty id and no encrypted_content still opens and
// closes its summary part, and the done metadata carries itemId "" and a null
// reasoningEncryptedContent like TS.
func TestResponsesStreamReasoningWithEmptyIDAndNoEncryptedContentClosesPart(t *testing.T) {
	body := `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"","summary":[]}}` + "\n\n" +
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"","summary":[]}}` + "\n\n"
	s := newResponsesStream(io.NopCloser(strings.NewReader(body)), false)
	var ended *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeReasoningEnd {
			ended = c
		}
	}
	if ended == nil || ended.ID != ":0" {
		t.Fatalf("reasoning-end = %+v", ended)
	}
	if got := string(ended.ProviderMetadata); got != `{"openai":{"itemId":"","reasoningEncryptedContent":null}}` {
		t.Fatalf("metadata = %s", got)
	}
}
