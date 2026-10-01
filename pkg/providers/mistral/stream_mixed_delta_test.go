package mistral

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// mixedThinkingTextSSE is a recorded thinking-mode stream where the first
// thinking part, a second thinking part and a text part arrive in the
// same SSE delta, followed by a delta that carries a tool call and the
// finish reason together.
const mixedThinkingTextSSE = `data: {"id":"cmpl-1","model":"magistral-medium-latest","created":1759300000,"choices":[{"index":0,"delta":{"role":"assistant","content":[{"type":"thinking","thinking":[{"type":"text","text":"Let me "}]},{"type":"thinking","thinking":[{"type":"text","text":"check."}]},{"type":"text","text":"The answer"},{"type":"text","text":" is 4."}]},"finish_reason":null}]}

data: {"id":"cmpl-1","model":"magistral-medium-latest","created":1759300000,"choices":[{"index":0,"delta":{"content":[{"type":"text","text":" Done."}],"tool_calls":[{"index":0,"id":"tc1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}

data: [DONE]

`

// TestMistralStreamMixedThinkingAndTextInOneDelta checks the TS order for a
// delta that mixes thinking and text parts: all reasoning first, then the
// joined text (ending reasoning). Before the fix, the text in the first
// delta was dropped once reasoning started.
func TestMistralStreamMixedThinkingAndTextInOneDelta(t *testing.T) {
	stream := newMistralStream(io.NopCloser(strings.NewReader(mixedThinkingTextSSE)))
	defer stream.Close() //nolint:errcheck

	var got []string
	var text, reasoning strings.Builder
	var sawToolCall, sawFinish bool
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		switch chunk.Type {
		case provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoningEnd:
			got = append(got, string(chunk.Type))
		case provider.ChunkTypeReasoning:
			got = append(got, "reasoning:"+chunk.Reasoning)
			reasoning.WriteString(chunk.Reasoning)
		case provider.ChunkTypeText:
			got = append(got, "text:"+chunk.Text)
			text.WriteString(chunk.Text)
		case provider.ChunkTypeToolCall:
			sawToolCall = true
		case provider.ChunkTypeFinish:
			sawFinish = true
		}
	}

	want := []string{
		string(provider.ChunkTypeReasoningStart),
		"reasoning:Let me ",
		"reasoning:check.",
		string(provider.ChunkTypeReasoningEnd),
		"text:The answer is 4.",
		"text: Done.",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chunk order:\n got  %q\n want %q", got, want)
	}
	if reasoning.String() != "Let me check." || text.String() != "The answer is 4. Done." {
		t.Fatalf("reasoning=%q text=%q", reasoning.String(), text.String())
	}
	if !sawToolCall || !sawFinish {
		t.Fatalf("tool call / finish in the same delta as text were lost: toolCall=%v finish=%v", sawToolCall, sawFinish)
	}
}
