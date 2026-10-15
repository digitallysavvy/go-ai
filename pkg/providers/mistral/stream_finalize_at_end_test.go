package mistral

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func drainMistral(t *testing.T, sse string) []*provider.StreamChunk {
	t.Helper()
	s := newMistralStream(io.NopCloser(strings.NewReader(sse)))
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			return out
		}
		out = append(out, c)
	}
}

// TS records finish_reason in transform() and finalizes tool calls, usage and
// the finish part in flush(), so deltas and x_groq usage after finish_reason
// still count; a finish_reason that shares a chunk with content is recorded.
func TestMistralStream_FinalizesAtStreamEnd(t *testing.T) {
	sse := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"f","arguments":"{\"a\":"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}` + "\n\n" +
		"data: [DONE]\n\n"
	chunks := drainMistral(t, sse)
	var call *provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolCall {
			call = c
		}
	}
	if call == nil {
		t.Fatalf("no tool call: %v", chunks)
	}
	if v, _ := call.ToolCall.Arguments["a"].(float64); v != 1 {
		t.Fatalf("args = %v", call.ToolCall.Arguments)
	}
	last := chunks[len(chunks)-1]
	if last.Type != provider.ChunkTypeFinish || last.FinishReason != types.FinishReasonToolCalls || last.Usage == nil || last.Usage.InputTokens == nil || *last.Usage.InputTokens != 3 {
		t.Fatalf("finish = %+v", last)
	}
}

func TestMistralStream_FinishReasonSharingChunkWithContent(t *testing.T) {
	sse := `data: {"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"length"}]}` + "\n\n" + "data: [DONE]\n\n"
	chunks := drainMistral(t, sse)
	last := chunks[len(chunks)-1]
	if last.Type != provider.ChunkTypeFinish || last.FinishReason != types.FinishReasonLength {
		t.Fatalf("finish = %+v", last)
	}
}
