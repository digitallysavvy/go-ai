package mistral

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r87eintegParts lists a stream's parts as "type" or "type:id", without
// response metadata and with the tool-input parts folded into "tool".
func r87eintegParts(t *testing.T, s provider.TextStream) []string {
	t.Helper()
	var out []string
	for {
		c, err := s.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		switch c.Type {
		case provider.ChunkTypeResponseMetadata:
			continue
		case provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputDelta, provider.ChunkTypeToolInputEnd:
			if len(out) == 0 || out[len(out)-1] != "tool" {
				out = append(out, "tool")
			}
			continue
		}
		p := string(c.Type)
		if c.ID != "" && c.Type != provider.ChunkTypeToolCall {
			p += ":" + c.ID
		}
		out = append(out, p)
	}
}

// TS mistral-chat-language-model.ts: text is block "0"; a thinking part ends
// it, text ends reasoning, flush ends text before the tool calls
// (cr-r87e-stream-to-provb class sweep).
func TestR87eintegMistralStreamTextBlocks(t *testing.T) {
	sse := `data: {"choices":[{"index":0,"delta":{"content":"a"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"content":[{"type":"thinking","thinking":[{"type":"text","text":"r"}]}]},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"content":"b","tool_calls":[{"index":0,"id":"c1","function":{"name":"f","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newMistralStream(io.NopCloser(strings.NewReader(sse)))
	stream.generateID = r90provgMockID() // TS mockId (reasoning block ids, r90)
	got := r87eintegParts(t, stream)
	want := []string{"text-start:0", "text:0", "text-end:0", "reasoning-start:id-0", "reasoning:id-0", "reasoning-end:id-0", "text-start:0", "text:0", "tool", "text-end:0", "tool", "tool-call", "finish"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parts\n got %q\nwant %q", got, want)
	}
}
