package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS validates the streamed content chunk and the done event's usage against
// zod, so a chunk without its required members is a parse error.
func TestConversationStreamRejectsMalformedChunkMembers(t *testing.T) {
	cases := map[string][]string{
		"text chunk without text":      {`{"type":"message.output.delta","id":"m","content":{"type":"text"}}`, `{"type":"conversation.response.done"}`},
		"thinking without array":       {`{"type":"message.output.delta","id":"m","content":{"type":"thinking","thinking":"x"}}`, `{"type":"conversation.response.done"}`},
		"thinking part without text":   {`{"type":"message.output.delta","id":"m","content":{"type":"thinking","thinking":[{"type":"text"}]}}`, `{"type":"conversation.response.done"}`},
		"tool_reference url not a str": {`{"type":"message.output.delta","id":"m","content":{"type":"tool_reference","url":5}}`, `{"type":"conversation.response.done"}`},
		"usage not an object":          {`{"type":"conversation.response.done","usage":"lots"}`},
		"usage token count not number": {`{"type":"conversation.response.done","usage":{"prompt_tokens":"1"}}`},
	}
	for name, events := range cases {
		t.Run(name, func(t *testing.T) {
			_, p := newConvRecorder(t, "text/event-stream", sse(events...))
			chunks := streamWith(t, p, &provider.GenerateOptions{Prompt: whoWon()})
			var sawError bool
			for _, c := range chunks {
				if c.Type == provider.ChunkTypeError {
					sawError = true
				}
			}
			last := chunks[len(chunks)-1]
			if !sawError || last.FinishReason != types.FinishReasonError {
				t.Errorf("types %v, last %#v", chunkTypes(chunks), last)
			}
		})
	}
}

// z.number() accepts fractional code / content_index / output_index.
func TestConversationStreamAcceptsFractionalNumbers(t *testing.T) {
	_, p := newConvRecorder(t, "text/event-stream", sse(
		`{"type":"message.output.delta","id":"m","content_index":1.5,"content":"hi"}`,
		`{"type":"function.call.delta","tool_call_id":"c","name":"f","arguments":"{}","output_index":0.0}`,
		`{"type":"conversation.response.done"}`))
	chunks := streamWith(t, p, &provider.GenerateOptions{Prompt: whoWon()})
	var id string
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("unexpected error chunk: %#v", c)
		}
		if c.Type == provider.ChunkTypeText {
			id = c.ID
		}
	}
	if id != "m-1.5" {
		t.Errorf("text id = %q, want m-1.5", id)
	}
}
