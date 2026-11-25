package alibaba

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

// TS alibaba-chat-language-model.ts: text is block "0"; it ends before a
// reasoning block starts, before tool call deltas and in flush
// (cr-r87e-stream-to-provb 1).
func TestR87eintegAlibabaStreamTextBlocks(t *testing.T) {
	sse := `data: {"choices":[{"index":0,"delta":{"content":"a"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"reasoning_content":"r"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"content":"b"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"content":"c"},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	got := r87eintegParts(t, newAlibabaStream(io.NopCloser(strings.NewReader(sse))))
	want := []string{"text-start:0", "text:0", "text-end:0", "reasoning-start:reasoning-0", "reasoning:reasoning-0", "reasoning-end:reasoning-0", "text-start:0", "text:0", "text-end:0", "tool", "text-start:0", "text:0", "text-end:0", "tool", "tool-call", "finish"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parts\n got %q\nwant %q", got, want)
	}
}
