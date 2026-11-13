package ai

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// stream-text.test.ts describe('programmatic tool calling') > 'should stop for
// client tool approval while a provider tool result is deferred'.
func TestTSReplay_ProgrammaticToolCalling_ApprovalWhileProviderResultDeferred(t *testing.T) {
	executed := 0
	calls := 0
	model := &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "program-call", ToolName: "program", Arguments: map[string]interface{}{"code": "getHours()"}, ProviderExecuted: true}},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "get-hours-call", ToolName: "getHours", Arguments: map[string]interface{}{"member": "Ada"}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: usage(3, 10)},
			}), nil
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Get Ada hours with a program.",
		Tools: []types.Tool{
			{Name: "program", ProviderExecuted: true, SupportsDeferredResults: true, Parameters: map[string]interface{}{"type": "object"}},
			{Name: "getHours", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"member": map[string]interface{}{"type": "string"}}},
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					executed++
					return nil, nil
				}},
		},
		ToolApproval: map[string]types.ToolApprovalValue{"getHours": types.ToolApprovalStatusUserApproval},
		StopWhen:     []StopCondition{IsStepCount(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var requests []string
	for {
		c, err := res.FullStream().Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("unexpected error chunk: %v", c.Text)
		}
		if c.Type == provider.ChunkTypeToolApprovalRequest {
			requests = append(requests, c.ToolApprovalRequest.ToolCall.ID+":"+c.ToolApprovalRequest.ToolCall.ToolName)
		}
	}
	if calls != 1 || executed != 0 || len(res.Steps()) != 1 {
		t.Fatalf("modelCalls=%d executed=%d steps=%d, want 1/0/1", calls, executed, len(res.Steps()))
	}
	if !reflect.DeepEqual(requests, []string{"get-hours-call:getHours"}) {
		t.Fatalf("approval requests = %v", requests)
	}
}

// stream-text.test.ts 'programmatic tool calling' > '5 steps: code_execution
// triggers client tool across multiple turns (dice game fixture)'.
func TestTSReplay_ProgrammaticToolCalling_DiceGame(t *testing.T) {
	const codeExecID = "srvtoolu_01MzSrFWsmzBdcoQkGWLyRjK"
	const containerID = "container_011CWHPPTDTn1XufeRB9uHeH"
	meta := json.RawMessage(`{"anthropic":{"container":{"id":"` + containerID + `"}}}`)
	rollCall := func(id, player string) provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: id, ToolName: "rollDie", Arguments: map[string]interface{}{"player": player}}}
	}
	toolStep := func(id, player string) []provider.StreamChunk {
		return []provider.StreamChunk{rollCall(id, player), {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: usage(0, 0), ProviderMetadata: meta}}
	}
	steps := [][]provider.StreamChunk{
		{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "I'll help you simulate this game."},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: codeExecID, ToolName: "code_execution", Arguments: map[string]interface{}{"code": "game_loop()"}, ProviderExecuted: true}},
			rollCall("toolu_1", "player1"),
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: usage(3369, 725), ProviderMetadata: meta},
		},
		toolStep("toolu_2", "player2"),
		toolStep("toolu_3", "player1"),
		toolStep("toolu_4", "player2"),
		{
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: codeExecID, ToolName: "code_execution", ProviderExecuted: true, Result: map[string]interface{}{"type": "code_execution_result", "stdout": "done", "return_code": 0}}},
			{Type: provider.ChunkTypeTextStart, ID: "2"},
			{Type: provider.ChunkTypeText, ID: "2", Text: "## Game Results"},
			{Type: provider.ChunkTypeTextEnd, ID: "2"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(4551, 197)},
		},
	}
	var mu sync.Mutex
	var prompts [][]types.Message
	var providerOpts []map[string]interface{}
	n := 0
	model := &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			defer mu.Unlock()
			prompts = append(prompts, o.Prompt.Messages)
			providerOpts = append(providerOpts, o.ProviderOptions)
			chunks := steps[n]
			n++
			return testutil.NewMockTextStream(chunks), nil
		},
	}
	var rolls []string
	var prepareMessages []int
	var prepareSteps []int
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Play a dice game between two players.",
		Tools: []types.Tool{
			{Name: "code_execution", ProviderExecuted: true, SupportsDeferredResults: true, Parameters: map[string]interface{}{"type": "object"}},
			{Name: "rollDie", Description: "Roll a die and return the result.", Parameters: map[string]interface{}{"type": "object"},
				Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
					p, _ := in["player"].(string)
					rolls = append(rolls, p)
					if p == "player1" {
						return 6, nil
					}
					return 3, nil
				}},
		},
		StopWhen: []StopCondition{IsStepCount(10)},
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			prepareMessages = append(prepareMessages, len(s.Messages))
			prepareSteps = append(prepareSteps, len(s.Steps))
			if s.StepNumber > 0 && len(s.Steps) > 0 {
				s.ProviderOptions = map[string]interface{}{"anthropic": map[string]interface{}{"container": map[string]interface{}{"id": containerID}}}
			}
			return s
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := len(res.Steps()); got != 5 {
		t.Fatalf("steps = %d, want 5", got)
	}
	for i, want := range []types.FinishReason{"tool-calls", "tool-calls", "tool-calls", "tool-calls", "stop"} {
		if got := res.Steps()[i].FinishReason; got != want {
			t.Fatalf("step %d finishReason = %v, want %v", i, got, want)
		}
	}
	if !reflect.DeepEqual(rolls, []string{"player1", "player2", "player1", "player2"}) {
		t.Fatalf("rollDie executions = %v", rolls)
	}
	if !reflect.DeepEqual(prepareMessages, []int{1, 3, 5, 7, 9}) || !reflect.DeepEqual(prepareSteps, []int{0, 1, 2, 3, 4}) {
		t.Fatalf("prepareStep messages=%v steps=%v", prepareMessages, prepareSteps)
	}
	if providerOpts[0]["anthropic"] != nil {
		t.Fatalf("step 1 providerOptions = %v, want none", providerOpts[0])
	}
	if !reflect.DeepEqual(providerOpts[1]["anthropic"], map[string]interface{}{"container": map[string]interface{}{"id": containerID}}) {
		t.Fatalf("step 2 providerOptions = %v", providerOpts[1])
	}
	// Step 2 prompt: user, assistant (text, provider call, client call), tool (rollDie result).
	p2 := prompts[1]
	if len(p2) != 3 || p2[1].Role != types.RoleAssistant || p2[2].Role != types.RoleTool {
		t.Fatalf("step 2 roles = %v", roles(p2))
	}
	if k := contentKinds(p2[1].Content); !reflect.DeepEqual(k, []string{"text", "tool-call", "tool-call"}) {
		t.Fatalf("step 2 assistant content = %v", k)
	}
	if len(p2[2].Content) != 1 {
		t.Fatalf("step 2 tool message content = %v", p2[2].Content)
	}
	if tr, ok := p2[2].Content[0].(types.ToolResultContent); !ok || tr.ToolName != "rollDie" {
		t.Fatalf("step 2 tool message = %#v", p2[2].Content[0])
	}
	// responseMessages: assistant/tool pairs for steps 1-4, final assistant with
	// the deferred provider tool result and the text.
	msgs := res.ResponseMessages()
	if len(msgs) != 9 {
		t.Fatalf("responseMessages = %d (%v), want 9", len(msgs), roles(msgs))
	}
	last := msgs[len(msgs)-1]
	if len(last.Content) != 2 {
		t.Fatalf("final assistant content = %v", last.Content)
	}
	if tr, ok := last.Content[0].(types.ToolResultContent); !ok || tr.ToolCallID != codeExecID || tr.Output == nil {
		t.Fatalf("final assistant content[0] = %#v, want the deferred provider tool result", last.Content[0])
	}
	if _, ok := last.Content[1].(types.TextContent); !ok {
		t.Fatalf("final assistant content[1] = %#v, want text", last.Content[1])
	}
	if res.Text() != "## Game Results" || res.FinishReason() != types.FinishReasonStop {
		t.Fatalf("text=%q finish=%v", res.Text(), res.FinishReason())
	}
	if got := len(res.ToolCalls()); got != 5 {
		t.Fatalf("toolCalls = %d, want 5", got)
	}
	if u := res.Usage(); u.InputTokens == nil || *u.InputTokens != 3369+4551 || *u.OutputTokens != 725+197 {
		t.Fatalf("total usage = %+v", u)
	}
	if fs := res.FinalStep(); len(fs.ToolCalls) != 0 {
		t.Fatalf("finalStep.toolCalls = %v", fs.ToolCalls)
	}
}
