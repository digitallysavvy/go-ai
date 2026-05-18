package deepseek

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func deepseekChunksOfType(chunks []*provider.StreamChunk, chunkType provider.ChunkType) []*provider.StreamChunk {
	var filtered []*provider.StreamChunk
	for _, chunk := range chunks {
		if chunk.Type == chunkType {
			filtered = append(filtered, chunk)
		}
	}
	return filtered
}

func TestDeepseekStream_TextChunks(t *testing.T) {
	sseData := `data: {"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	stream := newDeepseekStream(io.NopCloser(strings.NewReader(sseData)))
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if chunks[0].Type != provider.ChunkTypeText || chunks[0].Text != "Hello" {
		t.Errorf("chunk[0]: got type=%v text=%q", chunks[0].Type, chunks[0].Text)
	}
	if chunks[1].Type != provider.ChunkTypeText || chunks[1].Text != " world" {
		t.Errorf("chunk[1]: got type=%v text=%q", chunks[1].Type, chunks[1].Text)
	}
	if chunks[2].Type != provider.ChunkTypeFinish {
		t.Errorf("chunk[2]: expected finish, got %v", chunks[2].Type)
	}
}

func TestDeepseekStream_IncludeRawChunksMatchesTypeScript(t *testing.T) {
	sseData := `data: {"id":"raw-1","created":1702657020,"model":"deepseek-chat","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":""}]}

data: {"id":"raw-2","created":1702657020,"model":"deepseek-chat","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	stream := newDeepseekStream(io.NopCloser(strings.NewReader(sseData)), true)
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
		t.Fatalf("second chunk type = %v, want response-metadata", chunk.Type)
	}
	if chunk.ResponseMetadata == nil || chunk.ResponseMetadata.ID != "raw-1" || chunk.ResponseMetadata.ModelID != "deepseek-chat" {
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

// TestDeepseekStream_ToolCallPartialJSONNotFinalized verifies tool calls are accumulated
// across deltas and only emitted at finish_reason, never based on JSON parsability.
func TestDeepseekStream_ToolCallPartialJSONNotFinalized(t *testing.T) {
	// The second chunk delivers {"ready":true} which is complete valid JSON.
	// The old (buggy) code would have emitted the tool call immediately.
	// The fix requires waiting until finish_reason in the final chunk.
	sseData := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"fn","arguments":""}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"ready\":true}"}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newDeepseekStream(io.NopCloser(strings.NewReader(sseData)))
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, chunk)
	}

	toolCalls := deepseekChunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", len(toolCalls))
	}
	if toolCalls[0].ToolCall.ID != "call_1" {
		t.Errorf("tool call id: got %q", toolCalls[0].ToolCall.ID)
	}
	if toolCalls[0].ToolCall.ToolName != "fn" {
		t.Errorf("tool call name: got %q", toolCalls[0].ToolCall.ToolName)
	}
	if toolCalls[0].ToolCall.Arguments["ready"] != true {
		t.Errorf("tool call arg ready: got %v", toolCalls[0].ToolCall.Arguments["ready"])
	}
	finishes := deepseekChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %d", len(finishes))
	}
	if finishes[0].FinishReason != types.FinishReasonToolCalls {
		t.Errorf("finish reason: got %v", finishes[0].FinishReason)
	}
}

// TestDeepseekStream_ToolCallFinalizedAtFlush verifies tool calls are emitted only at stream end.
func TestDeepseekStream_ToolCallFinalizedAtFlush(t *testing.T) {
	sseData := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"calc","arguments":"{\"op\":"}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"add\"}"}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newDeepseekStream(io.NopCloser(strings.NewReader(sseData)))
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, chunk)
	}

	toolCalls := deepseekChunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", len(toolCalls))
	}
	if toolCalls[0].ToolCall.Arguments["op"] != "add" {
		t.Errorf("expected op=add, got %v", toolCalls[0].ToolCall.Arguments["op"])
	}
	if finishes := deepseekChunksOfType(chunks, provider.ChunkTypeFinish); len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %d", len(finishes))
	}
}

func TestDeepseekV4PreservesAssistantReasoningContent(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "deepseek-v4")

	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: "How many r's are in strawberry?"},
				},
			},
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ReasoningContent{Text: "Count each letter. "},
					types.ReasoningContent{Text: "There are three."},
					types.TextContent{Text: "3"},
				},
			},
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.TextContent{Text: "No hidden reasoning here."},
				},
			},
		}},
	}, false)

	messages := body["messages"].([]map[string]interface{})
	firstAssistant := messages[1]
	if got := firstAssistant["reasoning_content"]; got != "Count each letter. There are three." {
		t.Fatalf("first assistant reasoning_content: got %q", got)
	}
	secondAssistant := messages[2]
	if got := secondAssistant["reasoning_content"]; got != "" {
		t.Fatalf("second assistant reasoning_content should be backfilled empty string, got %q", got)
	}
}

func TestDeepseekNonV4OmitsAssistantReasoningContent(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "deepseek-reasoner")

	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ReasoningContent{Text: "Do not resend this to R1."},
					types.TextContent{Text: "answer"},
				},
			},
		}},
	}, false)

	messages := body["messages"].([]map[string]interface{})
	if _, ok := messages[0]["reasoning_content"]; ok {
		t.Fatalf("non-v4 DeepSeek messages should omit reasoning_content, got %#v", messages[0]["reasoning_content"])
	}
}
