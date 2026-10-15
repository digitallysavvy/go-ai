package streaming

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func compatSSE(chunks ...string) string {
	return "data: " + strings.Join(chunks, "\n\ndata: ") + "\n\ndata: [DONE]\n\n"
}

// TS openai-compatible-chat-language-model.test.ts stream cases that the
// replayed fixtures do not cover.
func TestOpenAICompatStream_TSEdgeCases(t *testing.T) {
	t.Run("empty role and content in delta", func(t *testing.T) {
		chunks := collectStreamChunks(t, newTestStream(compatSSE(
			`{"choices":[{"index":0,"delta":{"role":"","content":"Hello"},"finish_reason":null}]}`,
			`{"choices":[{"index":0,"delta":{"role":"","content":" World"},"finish_reason":null}]}`,
			`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)))
		var text string
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeText {
				text += c.Text
			}
		}
		if text != "Hello World" {
			t.Fatalf("text = %q", text)
		}
	})

	t.Run("reasoning stays active across empty tool_calls arrays", func(t *testing.T) {
		chunks := collectStreamChunks(t, newTestStream(compatSSE(
			`{"choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":"Think ","tool_calls":[]},"finish_reason":null}]}`,
			`{"choices":[{"index":0,"delta":{"content":"","reasoning_content":"more...","tool_calls":[]},"finish_reason":null}]}`,
			`{"choices":[{"index":0,"delta":{"content":"Hello","reasoning_content":"","tool_calls":[]},"finish_reason":"stop"}]}`)))
		var seq []string
		for _, c := range chunks {
			switch c.Type {
			case provider.ChunkTypeReasoningStart:
				seq = append(seq, "start")
			case provider.ChunkTypeReasoning:
				seq = append(seq, "delta:"+c.Reasoning)
			case provider.ChunkTypeReasoningEnd:
				seq = append(seq, "end")
			}
		}
		if got := strings.Join(seq, ","); got != "start,delta:Think ,delta:more...,end" {
			t.Fatalf("reasoning = %s", got)
		}
	})

	t.Run("id-less tool calls stay distinct when the index is reused", func(t *testing.T) {
		call := func(name, path string) string {
			return `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"type":"function","function":{"name":"` + name + `","arguments":"{\"path\":\"` + path + `\"}"}}]},"finish_reason":null}]}`
		}
		chunks := collectStreamChunks(t, newTestStream(compatSSE(
			call("read_file", "p0"), call("write_file", "p1"), call("read_file", "p2"),
			`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)))
		var names, ids []string
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeToolCall {
				names = append(names, c.ToolCall.ToolName)
				ids = append(ids, c.ToolCall.ID)
			}
		}
		if strings.Join(names, ",") != "read_file,write_file,read_file" {
			t.Fatalf("names = %v", names)
		}
		if ids[0] == ids[1] || ids[1] == ids[2] || ids[0] == ids[2] || ids[0] == "" {
			t.Fatalf("ids = %v", ids)
		}
	})

	t.Run("tool call without function.name errors", func(t *testing.T) {
		chunks := collectStreamChunks(t, newTestStream(compatSSE(
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_missing","type":"function","function":{"arguments":"{}"}}]},"finish_reason":null}]}`,
			`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)))
		var errText string
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeError {
				errText = c.Text
			}
		}
		if !strings.Contains(errText, "Expected 'function.name' to be a string.") {
			t.Fatalf("error = %q (chunks %d)", errText, len(chunks))
		}
	})

	t.Run("thought signature on streamed tool calls", func(t *testing.T) {
		stream := newTestStream(compatSSE(
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-paris","type":"function","function":{"name":"get_weather","arguments":""},"extra_content":{"google":{"thought_signature":"<Signature A>"}}},{"index":1,"id":"call-london","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"l\":\"Paris\"}"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"l\":\"London\"}"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`))
		stream.MetadataKey = "togetherai"
		chunks := collectStreamChunks(t, stream)
		var calls []*provider.StreamChunk
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeToolCall {
				calls = append(calls, c)
			}
		}
		if len(calls) != 2 || calls[0].ToolCall.ID != "call-paris" || calls[1].ToolCall.ID != "call-london" {
			t.Fatalf("calls = %+v", calls)
		}
		if md, _ := calls[0].ToolCall.ProviderMetadata["togetherai"].(map[string]interface{}); md["thoughtSignature"] != "<Signature A>" || len(calls[1].ToolCall.ProviderMetadata) != 0 {
			t.Fatalf("metadata = %v / %v", calls[0].ToolCall.ProviderMetadata, calls[1].ToolCall.ProviderMetadata)
		}
		if calls[0].ToolCall.ThoughtSignature != "<Signature A>" || calls[1].ToolCall.ThoughtSignature != "" {
			t.Fatalf("signatures = %q / %q", calls[0].ToolCall.ThoughtSignature, calls[1].ToolCall.ThoughtSignature)
		}
	})
}
