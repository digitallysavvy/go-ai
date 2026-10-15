package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// scriptedModel answers call 1 with the given tool calls and later calls with
// text, recording the prompt messages of every call.
type scriptedModel struct {
	wfMockModel
	mu      sync.Mutex
	calls   []types.ToolCall
	prompts [][]types.Message
	n       int
}

func (m *scriptedModel) record(o *provider.GenerateOptions) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n++
	m.prompts = append(m.prompts, append([]types.Message(nil), o.Prompt.Messages...))
	return m.n == 1
}

func (m *scriptedModel) DoGenerate(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
	if m.record(o) {
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: m.calls}, nil
	}
	return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
}

func (m *scriptedModel) DoStream(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
	if m.record(o) {
		var chunks []provider.StreamChunk
		for i := range m.calls {
			c := m.calls[i]
			chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &c})
		}
		chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls})
		return testutil.NewMockTextStream(chunks), nil
	}
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "done"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}), nil
}

func toolMessagesJSON(m *scriptedModel) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.prompts) < 2 {
		return fmt.Sprintf("only %d calls", len(m.prompts))
	}
	var out []types.Message
	for _, msg := range m.prompts[1] {
		if msg.Role == types.RoleTool || msg.Role == types.RoleAssistant {
			out = append(out, msg)
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func TestWorkflowGenerateAndStreamAgreeOnToolEdgeCases(t *testing.T) {
	schemaTool := func(exec types.ToolExecutor) types.Tool {
		return types.Tool{
			Name: "t", Description: "d",
			Parameters: map[string]interface{}{
				"type": "object", "required": []interface{}{"x"},
				"properties": map[string]interface{}{"x": map[string]interface{}{"type": "string"}},
			},
			Execute: exec,
		}
	}
	cases := map[string]struct {
		calls []types.ToolCall
		tool  types.Tool
	}{
		"tool error": {
			calls: []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{"x": "a"}}},
			tool: schemaTool(func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return nil, errors.New("kaboom")
			}),
		},
		"unknown tool": {
			calls: []types.ToolCall{{ID: "1", ToolName: "nope", Arguments: map[string]interface{}{}}},
			tool: schemaTool(func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			}),
		},
		"invalid input": {
			calls: []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{"x": 5}}},
			tool: schemaTool(func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			}),
		},
		"object result": {
			calls: []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{"x": "a"}}},
			tool: schemaTool(func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"a": 1}, nil
			}),
		},
		"no execute": {
			calls: []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{"x": "a"}}},
			tool:  schemaTool(nil),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			run := func(stream bool) (string, int) {
				model := &scriptedModel{calls: tc.calls}
				w, err := NewWorkflowAgent(WorkflowAgent{Model: model, Tools: []types.Tool{tc.tool}})
				if err != nil {
					t.Fatal(err)
				}
				steps := 0
				if stream {
					res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
					if err != nil {
						t.Fatal(err)
					}
					if e := res.ConsumeStream(); e != nil { t.Logf("consume err: %v", e) }
					steps = len(res.Steps())
				} else {
					res, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"})
					if err != nil {
						t.Fatalf("generate: %v", err)
					}
					steps = len(res.Steps)
				}
				return toolMessagesJSON(model), steps
			}
			g, gs := run(false)
			s, ss := run(true)
			if g != s || gs != ss {
				t.Fatalf("generate/stream differ\n gen(%d steps): %s\n str(%d steps): %s", gs, g, ss, s)
			}
		})
	}
}
