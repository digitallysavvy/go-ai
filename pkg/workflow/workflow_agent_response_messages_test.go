package workflow

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type fnModel struct {
	wfMockModel
	gen    func() *types.GenerateResult
	chunks []provider.StreamChunk
}

func (m *fnModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	return m.gen(), nil
}

func (m *fnModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	return testutil.NewMockTextStream(m.chunks), nil
}

// TS #22018, #22020, #22396, #22093: partial responses that end with a
// non-stop finish reason are kept in the step's response messages (generate and
// stream), text provider metadata is kept as provider options, and
// provider-executed tool calls/results survive a length stop.
func TestWorkflowPartialResponsesStayInResponseMessages(t *testing.T) {
	meta := []byte(`{"openai":{"itemId":"msg_1"}}`)
	for _, fr := range []types.FinishReason{types.FinishReasonLength, types.FinishReasonContentFilter, types.FinishReasonError, types.FinishReasonOther} {
		fr := fr
		t.Run(string(fr), func(t *testing.T) {
			m := &fnModel{
				gen: func() *types.GenerateResult {
					return &types.GenerateResult{Text: "partial", FinishReason: fr,
						Content: []types.ContentPart{types.TextContent{Text: "partial", ProviderMetadata: meta}}}
				},
				chunks: []provider.StreamChunk{
					{Type: provider.ChunkTypeTextStart, ID: "0", ProviderMetadata: meta},
					{Type: provider.ChunkTypeText, ID: "0", Text: "partial"},
					{Type: provider.ChunkTypeTextEnd, ID: "0", ProviderMetadata: meta},
					{Type: provider.ChunkTypeFinish, FinishReason: fr},
				},
			}
			a, err := NewWorkflowAgent(WorkflowAgent{Model: m})
			if err != nil {
				t.Fatal(err)
			}
			check := func(t *testing.T, steps []types.StepResult) {
				t.Helper()
				if len(steps) != 1 || len(steps[0].Response.Messages) != 1 {
					t.Fatalf("steps = %+v", steps)
				}
				msg := steps[0].Response.Messages[0]
				text, ok := msg.Content[0].(types.TextContent)
				if msg.Role != types.RoleAssistant || !ok || text.Text != "partial" {
					t.Fatalf("message = %+v", msg)
				}
				if text.ProviderOptions["openai"] == nil {
					t.Fatalf("text provider metadata dropped: %+v", text)
				}
			}
			res, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			check(t, res.Steps)
			st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.ReadAll(); err != nil {
				t.Fatal(err)
			}
			check(t, st.Steps())
		})
	}
}

func TestWorkflowLengthStopKeepsProviderExecutedToolResults(t *testing.T) {
	m := &fnModel{chunks: []provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "p1", ToolName: "web_search", Arguments: map[string]interface{}{"q": "x"}, ProviderExecuted: true}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "p1", ToolName: "web_search", Result: "found", ProviderExecuted: true}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonLength},
	}}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{{Name: "web_search", ProviderExecuted: true}}})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadAll(); err != nil {
		t.Fatal(err)
	}
	var call, result bool
	for _, s := range st.Steps() {
		for _, msg := range s.Response.Messages {
			for _, p := range msg.Content {
				switch p.(type) {
				case types.ToolCallContent:
					call = true
				case types.ToolResultContent:
					result = true
				}
			}
		}
	}
	if !call || !result {
		t.Fatalf("provider-executed call=%v result=%v, want both", call, result)
	}
}

func TestWorkflowIncompleteStreamWithoutOutputReportsError(t *testing.T) {
	m := &fnModel{chunks: []provider.StreamChunk{}}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: m})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := st.ReadAll()
	t.Logf("readErr=%v streamErr=%v", readErr, st.Err())
	if readErr == nil && st.Err() == nil {
		t.Fatal("expected a no-output error for a stream that ended without output or finish")
	}
}
