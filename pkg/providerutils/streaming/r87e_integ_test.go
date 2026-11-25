package streaming

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS openai-compatible-chat-language-model.ts: text is one "txt-0" block;
// a reasoning delta ends it (enqueueReasoningDelta), flush ends the last
// one before the tool calls (cr-r87e-stream-to-provb class sweep).
func TestR87eintegOpenAICompatStreamTextBlocks(t *testing.T) {
	sse := `data: {"choices":[{"delta":{"content":"a"},"finish_reason":null}]}

data: {"choices":[{"delta":{"reasoning_content":"r"},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":"b","tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	var got []string
	for _, c := range collectStreamChunks(t, newTestStream(sse)) {
		switch c.Type {
		case provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputDelta, provider.ChunkTypeToolInputEnd:
			continue
		}
		got = append(got, string(c.Type)+":"+c.ID)
	}
	want := []string{"text-start:txt-0", "text:txt-0", "text-end:txt-0", "reasoning-start:reasoning-0", "reasoning:reasoning-0", "reasoning-end:reasoning-0", "text-start:txt-0", "text:txt-0", "text-end:txt-0", "tool-call:", "finish:"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parts\n got %q\nwant %q", got, want)
	}
}
