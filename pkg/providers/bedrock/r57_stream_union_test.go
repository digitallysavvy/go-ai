package bedrock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r57: the TS delta union picks the first member that parses. A toolUse
// whose input is not a string is not the toolUse member, so a
// reasoningContent beside it is the matching member and streams as reasoning.
func TestR57BedrockStreamToolUseWithoutStringInputFallsToReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(buildBedrockEventStreamBody([][2]string{
			{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"toolUse":{"input":5},"reasoningContent":{"text":"think"}}}`},
			{"contentBlockStop", `{"contentBlockIndex":0}`},
			{"messageStop", `{"stopReason":"end_turn"}`},
		}))
	}))
	defer server.Close()
	model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var reasoning string
	for _, c := range drainChunks(t, stream) {
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("unexpected error chunk: %v", c.Err)
		}
		if c.Type == provider.ChunkTypeReasoning {
			reasoning += c.Reasoning
		}
	}
	if reasoning != "think" {
		t.Fatalf("reasoning = %q, want %q", reasoning, "think")
	}
}

// r57: TS filters tool content only when the prompt holds a tool call or
// result; with none, the prompt goes to the converter unchanged (an empty
// assistant message is not dropped) and no toolContent warning is added.
func TestR57BedrockNoToolsWithoutToolContentKeepsPrompt(t *testing.T) {
	model := NewLanguageModel(&Provider{}, "amazon.nova-pro-v1:0")
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "a"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "b"}}},
	}
	got, removed := filterToolContent(msgs)
	if removed || len(got) != 3 {
		t.Fatalf("filterToolContent = %d messages, removed=%v; want the 3 messages unchanged", len(got), removed)
	}
	args, err := model.getArgs(&provider.GenerateOptions{Prompt: types.Prompt{Messages: msgs}})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range args.Warnings {
		if w.Feature == "toolContent" {
			t.Fatalf("unexpected toolContent warning: %+v", w)
		}
	}
	// With tool content, empty messages are dropped together with the parts.
	withTool := append([]types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c", ToolName: "t"}}}}, msgs...)
	got, removed = filterToolContent(withTool)
	if !removed || len(got) != 2 {
		t.Fatalf("filterToolContent with a tool call = %d messages, removed=%v; want 2, true", len(got), removed)
	}
}
