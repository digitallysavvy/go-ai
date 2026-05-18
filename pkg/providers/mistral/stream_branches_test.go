package mistral

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestMistralStreamReasoningToolCallsAndErr(t *testing.T) {
	sse := `data: {"choices":[{"delta":{"content":[{"type":"thinking","thinking":[{"type":"text","text":"think-1"}]}]},"finish_reason":""}]}

data: {"choices":[{"delta":{"content":[{"type":"text","text":"hello"}]},"finish_reason":""}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"tc1","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}}]},"finish_reason":""}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]},"finish_reason":""}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newMistralStream(io.NopCloser(strings.NewReader(sse)))
	defer stream.Close() //nolint:errcheck

	var gotReasoningStart, gotReasoning, gotReasoningEnd, gotTool, gotFinish bool
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		switch chunk.Type {
		case provider.ChunkTypeReasoningStart:
			gotReasoningStart = true
		case provider.ChunkTypeReasoning:
			gotReasoning = true
		case provider.ChunkTypeReasoningEnd:
			gotReasoningEnd = true
		case provider.ChunkTypeToolCall:
			gotTool = true
			if chunk.ToolCall.ToolName != "lookup" || chunk.ToolCall.Arguments["q"] != "x" {
				t.Fatalf("tool call mismatch: %#v", chunk.ToolCall)
			}
		case provider.ChunkTypeFinish:
			gotFinish = true
			if chunk.FinishReason != types.FinishReasonToolCalls {
				t.Fatalf("finish reason mismatch: %v", chunk.FinishReason)
			}
		}
	}
	if !gotReasoningStart || !gotReasoning || !gotReasoningEnd || !gotTool || !gotFinish {
		t.Fatalf("missing expected chunks: reasoningStart=%v reasoning=%v reasoningEnd=%v tool=%v finish=%v", gotReasoningStart, gotReasoning, gotReasoningEnd, gotTool, gotFinish)
	}
	if stream.Err() != nil {
		t.Fatalf("Err() should be nil after EOF, got %v", stream.Err())
	}
}

func TestMistralStreamIncludeRawChunksMatchesTypeScript(t *testing.T) {
	sse := `data: {"id":"raw-1","object":"chat.completion.chunk","created":1750538600,"model":"mistral-large-latest","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null,"logprobs":null}]}

data: {"id":"raw-2","object":"chat.completion.chunk","created":1750538601,"model":"mistral-large-latest","choices":[{"index":0,"delta":{},"finish_reason":"stop","logprobs":null}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}

data: [DONE]

`
	stream := newMistralStream(io.NopCloser(strings.NewReader(sse)), true)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("first chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeRaw {
		t.Fatalf("first chunk type = %v, want raw", chunk.Type)
	}
	raw, ok := chunk.Raw.(map[string]interface{})
	if !ok || raw["id"] != "raw-1" {
		t.Fatalf("raw chunk = %#v, want id raw-1", chunk.Raw)
	}

	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("second chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeResponseMetadata {
		t.Fatalf("second chunk = %#v, want response metadata", chunk)
	}
	if chunk.ResponseMetadata == nil || chunk.ResponseMetadata.ID != "raw-1" || chunk.ResponseMetadata.ModelID != "mistral-large-latest" {
		t.Fatalf("response metadata = %#v, want first provider event metadata", chunk.ResponseMetadata)
	}

	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("third chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeText || chunk.Text != "Hello" {
		t.Fatalf("third chunk = %#v, want text Hello", chunk)
	}
}

func TestMistralFlushToolCallsReasoningEnd(t *testing.T) {
	s := &mistralStream{
		toolCallAccum: map[int]*mistralStreamAccumToolCall{
			0: {id: "tc1", name: "lookup", arguments: `{"ok":true}`},
		},
		isActiveReasoning: true,
	}
	s.flushMistralToolCalls("stop")
	if len(s.flushQueue) < 3 {
		t.Fatalf("expected reasoning_end + tool_call + finish, got %#v", s.flushQueue)
	}
	if s.flushQueue[0].Type != provider.ChunkTypeReasoningEnd {
		t.Fatalf("first chunk should be reasoning end, got %v", s.flushQueue[0].Type)
	}
	last := s.flushQueue[len(s.flushQueue)-1]
	if last.Type != provider.ChunkTypeFinish || last.FinishReason != types.FinishReasonStop {
		t.Fatalf("finish chunk mismatch: %#v", last)
	}
}
