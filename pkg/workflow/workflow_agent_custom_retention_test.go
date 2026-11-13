package workflow

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// customLoopModel emits a custom part plus a tool call on step one, then
// answers, recording the messages of every call.
type customLoopModel struct {
	wfMockModel
	custom types.CustomContent
	mu     sync.Mutex
	calls  int
	msgs   [][]types.Message
}

func (m *customLoopModel) DoStream(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	m.mu.Lock()
	m.calls++
	call := m.calls
	m.msgs = append(m.msgs, append([]types.Message(nil), opts.Prompt.Messages...))
	m.mu.Unlock()
	if call == 1 {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeCustom, CustomContent: &m.custom},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "search", Arguments: map[string]interface{}{}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "t"},
		{Type: provider.ChunkTypeText, ID: "t", Text: "done"},
		{Type: provider.ChunkTypeTextEnd, ID: "t"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}), nil
}

// TS #23799: WorkflowAgent.stream retains provider-specific custom content
// parts (e.g. openai.compaction) in the step result, the response
// messages and the next model request.
func TestWorkflowStreamRetainsCustomContentParts(t *testing.T) {
	meta := json.RawMessage(`{"openai":{"itemId":"cmp_1","encryptedContent":"encrypted"}}`)
	m := &customLoopModel{custom: types.CustomContent{Kind: "openai.compaction", ProviderMetadata: meta}}
	tool := types.Tool{Name: "search", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}
	a, _ := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{tool}})
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{
		Messages: []types.Message{userMsg("question")},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.ConsumeStream()

	steps := st.Steps()
	if len(steps) != 2 {
		t.Fatalf("steps = %d", len(steps))
	}
	isCustom := func(p types.ContentPart) bool {
		c, ok := p.(types.CustomContent)
		return ok && c.Kind == "openai.compaction" && string(c.ProviderMetadata) == string(meta)
	}
	found := false
	for _, p := range steps[0].Content {
		found = found || isCustom(p)
	}
	if !found {
		t.Fatalf("step content = %#v, want the custom part", steps[0].Content)
	}
	hasCustom := func(msgs []types.Message) bool {
		for _, msg := range msgs {
			if msg.Role != types.RoleAssistant {
				continue
			}
			for _, p := range msg.Content {
				if c, ok := p.(types.CustomContent); ok && c.Kind == "openai.compaction" {
					return true
				}
			}
		}
		return false
	}
	if !hasCustom(steps[0].Response.Messages) {
		t.Fatalf("response messages = %#v, want the custom part", steps[0].Response.Messages)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.msgs) != 2 || !hasCustom(m.msgs[1]) {
		t.Fatalf("second model request = %#v, want the custom part", m.msgs)
	}
}
