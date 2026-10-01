package agent

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestExecuteWithMessagesToolCallers_AnnouncementPersistsAcrossSteps is a
// regression test for the tool-caller message persistence bug: TS
// generate-text.ts builds each step's `messagesForNextStep` from that
// step's own `stepMessages` (the messages *after* appendToolCallerMessages
// ran), so a local tool caller's PrepareModelMessage announcement persists
// into later steps' history instead of being dropped after one step.
// executeWithMessages (used by ToolLoopAgent.GenerateAgent) previously
// carried forward currentMessages from *before* AppendToolCallerMessages
// ran each step, so a step's own announcement never made it into
// currentMessages and was lost once that step's response messages were
// appended.
//
// The announcement text changes on every step (like code-mode's
// "conversation" discovery catalog growing as tools are discovered) so
// AppendToolCallerMessages's same-text dedup can't mask the bug: a stable,
// unchanging announcement would be deduped against itself on every step
// regardless of whether it was actually persisted. The bug only surfaces at
// step 3: step 2's own prompt is always correct (callConfig.Messages is
// built fresh each step and sent directly to the model), but persisting
// that value into currentMessages for step 3 is exactly what the fix
// restores.
func TestExecuteWithMessagesToolCallers_AnnouncementPersistsAcrossSteps(t *testing.T) {
	announcementFor := func(n int) string {
		return fmt.Sprintf("Available caller tools (v%d): getInventory.", n)
	}
	announceCalls := 0
	callerTool := types.Tool{
		Name:       "code_mode",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("caller was not bound")
		},
		ExperimentalToolCaller: &types.ToolCallerDefinition{
			Type: types.ToolCallerTypeLocal,
			Bind: func(tools []types.Tool) types.Tool {
				return types.Tool{
					Name:       "code_mode",
					Parameters: map[string]interface{}{"type": "object"},
					Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
						names := make([]string, 0, len(tools))
						for _, tl := range tools {
							names = append(names, tl.Name)
						}
						return names, nil
					},
				}
			},
			PrepareModelMessage: func([]types.Tool) *string {
				announceCalls++
				text := announcementFor(announceCalls)
				return &text
			},
		},
	}
	calleeTool := types.Tool{
		Name:       "getInventory",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"availableUnits": 42}, nil
		},
	}

	var prompts [][]types.Message
	calls := 0
	model := &functionalAgentLanguageModel{
		doGenerate: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			prompts = append(prompts, opts.Prompt.Messages)
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{
					{ID: fmt.Sprintf("call-%d", calls), ToolName: "code_mode", Arguments: map[string]interface{}{}},
				},
			}, nil
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model:    model,
		Tools:    []types.Tool{callerTool, calleeTool},
		MaxSteps: 3,
		ExperimentalToolCallers: ai.ExperimentalToolCallers{
			"getInventory": {"code_mode"},
		},
	})

	if _, err := agent.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "Check inventory."}); err != nil {
		t.Fatalf("GenerateAgent() error = %v", err)
	}
	if len(prompts) != 3 {
		t.Fatalf("model called %d times, want 3", len(prompts))
	}

	step1, step2, step3 := prompts[0], prompts[1], prompts[2]
	assertAgentAnnouncementMessage(t, step1, announcementFor(1))
	assertAgentAnnouncementMessage(t, step2, announcementFor(2))

	// Ports the shape of TS's
	// `expect(second.prompt.slice(0, first.prompt.length)).toEqual(first.prompt)`
	// (code-mode/src/tool-search.test.ts, also ported directly in
	// pkg/codemode/tool_search_integration_test.go): each step's prompt
	// must be an exact prefix-extension of the previous step's.
	if len(step2) < len(step1) {
		t.Fatalf("step2 prompt has %d messages, want at least %d (step1 length)", len(step2), len(step1))
	}
	if !reflect.DeepEqual(step2[:len(step1)], step1) {
		t.Fatalf("step2 prompt prefix = %+v, want %+v (step1 prompt)", step2[:len(step1)], step1)
	}
	if len(step3) < len(step2) {
		t.Fatalf("step3 prompt has %d messages, want at least %d (step2 length)", len(step3), len(step2))
	}
	if !reflect.DeepEqual(step3[:len(step2)], step2) {
		t.Fatalf("step3 prompt prefix = %+v, want %+v (step2 prompt)", step3[:len(step2)], step2)
	}
}

func assertAgentAnnouncementMessage(t *testing.T, messages []types.Message, announcement string) {
	t.Helper()
	for _, m := range messages {
		if m.Role != types.RoleUser || len(m.Content) != 1 {
			continue
		}
		if tc, ok := m.Content[0].(types.TextContent); ok && tc.Text == announcement {
			return
		}
	}
	t.Fatalf("expected announcement message %q in %+v", announcement, messages)
}
