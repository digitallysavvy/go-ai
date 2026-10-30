package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r71genJSON is v marshaled to JSON and decoded back into plain values (the
// TS shape that MarshalJSON emits), without Go's extra top-level tool_calls
// message field, so it compares with a TS inline snapshot.
func r71genJSON(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if msgs, ok := out.([]interface{}); ok {
		for _, m := range msgs {
			if mm, ok := m.(map[string]interface{}); ok {
				delete(mm, "tool_calls")
			}
		}
	}
	return out
}

func r71genCheckJSON(t *testing.T, name string, got, want interface{}) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		g, _ := json.MarshalIndent(got, "", " ")
		w, _ := json.MarshalIndent(want, "", " ")
		t.Errorf("%s =\n%s\nwant\n%s", name, g, w)
	}
}

type r71genObj = map[string]interface{}

const (
	r71genCodeExecID  = "srvtoolu_01CberhXc9TgYXrCZU8bQoks"
	r71genContainerID = "container_011CWHQB57xVregfCMPrKgew"
	r71genIntro       = "I'll help you simulate this dice game between two players! Let me run the game where both players roll dice each round until one player wins 3 rounds."
	r71genStdout      = "============================================================\nDICE GAME: First to 3 Round Wins\n============================================================\n\nRound 1:\n  Player 1 rolls: 6\n  Player 2 rolls: 6\n  → Draw! No points awarded.\n  Score: Player 1: 0 | Player 2: 0\n"
	r71genFinalText   = "**Game Over!**\n\nPlayer 1 dominated this game with a decisive 3-0 victory!"
)

// The dice game rounds: per step, the two rollDie calls (id, player).
var r71genRounds = [][2][2]string{
	{{"toolu_01PMcE1JBKCeLjn83cgUCvR5", "player2"}, {"toolu_01MZf5QJ1EQyd2yGyeLzBxAS", "player1"}},
	{{"toolu_01UvVQ2xwA6preZppeajCkYK", "player1"}, {"toolu_01BghspNownQFtRgv8jVicr3", "player2"}},
	{{"toolu_01T7Upuuv8C71nq7DZ9ZPNQW", "player1"}, {"toolu_016Da1tDet9Bf7dAdYTkF5Ar", "player2"}},
	{{"toolu_01DiUBRds64sNajVPTZRrDSM", "player1"}, {"toolu_01XQa3r3y1Fe8rnkGSncq626", "player2"}},
}

func r71genRoll(player string) int {
	if player == "player1" {
		return 6
	}
	return 3
}

// r71genDiceResponses is the five model responses of the TS fixture
// (anthropic-programmatic-tool-calling.1.json as generate-text.test.ts mocks
// it). The code_execution input is {"code": ...}: the TS fixture's extra
// "type" key is dropped there only because its zod inputSchema strips
// unknown keys, which a JSON Schema does not do.
func r71genDiceResponses() []*types.GenerateResult {
	i64 := func(v int64) *int64 { return &v }
	use := func(in, out int64) types.Usage {
		return types.Usage{InputTokens: i64(in), InputDetails: &types.InputTokenDetails{NoCacheTokens: i64(in)}, OutputTokens: i64(out), OutputDetails: &types.OutputTokenDetails{TextTokens: i64(out)}, TotalTokens: i64(in + out)}
	}
	meta := func() map[string]interface{} {
		return r71genObj{"anthropic": r71genObj{"container": r71genObj{"id": r71genContainerID}}}
	}
	response := func(id string, ms int64) *types.ResponseMetadata {
		return &types.ResponseMetadata{ID: id, Timestamp: time.UnixMilli(ms), ModelID: "claude-sonnet-4-5-20250929"}
	}
	ids := []string{"msg_01V3gktwqnMAsXxpJ6sG9KDR", "msg_01DmEmveJkWRUR1y41DfGfEQ", "msg_01AxicwpZwTqKCtty95VTAoL", "msg_01TjXmSMrfqKVMpHHM3wMaBy", "msg_01LPPbjvqcSgBPfApMEiJ4qv"}
	var out []*types.GenerateResult
	for n, round := range r71genRounds {
		r := &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, Usage: use(0, 0), ProviderMetadata: meta(), ResponseMetadata: response(ids[n], int64(n)*1000)}
		if n == 0 {
			r.Usage = use(3369, 577)
			code := types.ToolCall{ID: r71genCodeExecID, ToolName: "code_execution", Arguments: r71genObj{"code": "game_loop()"}, RawArguments: `{"code":"game_loop()"}`, ProviderExecuted: true}
			r.Content = append(r.Content, types.TextContent{Text: r71genIntro},
				types.ToolCallContent{ToolCallID: code.ID, ToolName: code.ToolName, Input: code.RawArguments, ProviderExecuted: true})
			r.ToolCalls = append(r.ToolCalls, code)
		}
		for _, c := range round {
			raw := `{ "player": "` + c[1] + `" }`
			r.Content = append(r.Content, types.ToolCallContent{ToolCallID: c[0], ToolName: "rollDie", Input: raw})
			r.ToolCalls = append(r.ToolCalls, types.ToolCall{ID: c[0], ToolName: "rollDie", Arguments: r71genObj{"player": c[1]}, RawArguments: raw})
		}
		out = append(out, r)
	}
	out = append(out, &types.GenerateResult{
		Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: r71genCodeExecID, ToolName: "code_execution", ProviderExecuted: true,
				Result: r71genObj{"type": "code_execution_result", "stdout": r71genStdout, "stderr": "", "return_code": float64(0), "content": []interface{}{}}},
			types.TextContent{Text: r71genFinalText},
		},
		FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: use(4243, 229), ResponseMetadata: response(ids[4], 4000),
	})
	return out
}

// r71genDiceResponseMessages is the TS result.responseMessages inline
// snapshot (undefined fields dropped).
func r71genDiceResponseMessages() []interface{} {
	call := func(id, name string, input r71genObj, providerExecuted bool) r71genObj {
		p := r71genObj{"type": "tool-call", "toolCallId": id, "toolName": name, "input": input}
		if providerExecuted {
			p["providerExecuted"] = true
		}
		return p
	}
	result := func(id, name string, value interface{}) r71genObj {
		return r71genObj{"type": "tool-result", "toolCallId": id, "toolName": name, "output": r71genObj{"type": "json", "value": value}}
	}
	var msgs []interface{}
	for n, round := range r71genRounds {
		var calls, results []interface{}
		if n == 0 {
			calls = append(calls, r71genObj{"type": "text", "text": r71genIntro}, call(r71genCodeExecID, "code_execution", r71genObj{"code": "game_loop()"}, true))
		}
		for _, c := range round {
			calls = append(calls, call(c[0], "rollDie", r71genObj{"player": c[1]}, false))
			results = append(results, result(c[0], "rollDie", float64(r71genRoll(c[1]))))
		}
		msgs = append(msgs, r71genObj{"role": "assistant", "content": calls}, r71genObj{"role": "tool", "content": results})
	}
	msgs = append(msgs, r71genObj{"role": "assistant", "content": []interface{}{
		result(r71genCodeExecID, "code_execution", r71genObj{"type": "code_execution_result", "stdout": r71genStdout, "stderr": "", "return_code": float64(0), "content": []interface{}{}}),
		r71genObj{"type": "text", "text": r71genFinalText},
	}})
	return msgs
}

// generate-text.test.ts 'programmatic tool calling' > '5 steps:
// code_execution triggers client tool across multiple turns (dice game
// fixture)': every describe block (step inputs, responseMessages,
// toolCalls/toolResults, tool execution, steps, text, finishReason,
// totalUsage, prepareStep calls, onStepFinish, onFinish).
func TestR71Gen_GenerateText_ProgrammaticToolCallingDiceGame(t *testing.T) {
	responses := r71genDiceResponses()
	var mu sync.Mutex
	var calls []*provider.GenerateOptions
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, o)
			return responses[len(calls)-1], nil
		}}
	type prepareCall struct {
		modelID    string
		stepNumber int
		steps      int
		messages   int
	}
	var prepareCalls []prepareCall
	var rolls [][]string
	var stepEvents []OnStepFinishEvent
	var endEvent OnFinishEvent
	rollDieOptions := r71genObj{"anthropic": r71genObj{"allowedCallers": []interface{}{"code_execution_20250825"}}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Play a dice game between two players.",
		Tools: []types.Tool{
			{Name: "code_execution", ProviderExecuted: true, SupportsDeferredResults: true,
				Parameters: r71genObj{"type": "object", "properties": r71genObj{"code": r71genObj{"type": "string"}}, "required": []interface{}{"code"}}},
			{Name: "rollDie", Description: "Roll a die and return the result.",
				Parameters:      r71genObj{"type": "object", "properties": r71genObj{"player": r71genObj{"type": "string", "enum": []interface{}{"player1", "player2"}}}, "required": []interface{}{"player"}, "additionalProperties": false},
				ProviderOptions: rollDieOptions,
				Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
					p, _ := in["player"].(string)
					mu.Lock()
					rolls[len(rolls)-1] = append(rolls[len(rolls)-1], p)
					mu.Unlock()
					return r71genRoll(p), nil
				}},
		},
		StopWhen: []StopCondition{IsStepCount(10)},
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			mu.Lock()
			prepareCalls = append(prepareCalls, prepareCall{modelID: s.Model.ModelID(), stepNumber: s.StepNumber, steps: len(s.Steps), messages: len(s.Messages)})
			rolls = append(rolls, nil)
			mu.Unlock()
			// Forward the container ID from the previous step (TS
			// forwardAnthropicContainerIdFromLastStep).
			if s.StepNumber > 0 && len(s.Steps) > 0 {
				last := s.Steps[len(s.Steps)-1]
				if a, ok := last.ProviderMetadata["anthropic"].(map[string]interface{}); ok {
					if c, ok := a["container"].(map[string]interface{}); ok && c["id"] != nil {
						s.ProviderOptions = r71genObj{"anthropic": r71genObj{"container": r71genObj{"id": c["id"]}}}
					}
				}
			}
			return s
		},
		OnStepEndEvent: func(_ context.Context, e OnStepFinishEvent) { stepEvents = append(stepEvents, e) },
		OnEndEvent:     func(_ context.Context, e OnFinishEvent) { endEvent = e },
	})
	if err != nil {
		t.Fatal(err)
	}

	// step inputs
	if len(calls) != 5 {
		t.Fatalf("model calls = %d, want 5", len(calls))
	}
	user := r71genObj{"role": "user", "content": []interface{}{r71genObj{"type": "text", "text": "Play a dice game between two players."}}}
	want := r71genDiceResponseMessages()
	for n, call := range calls {
		// Step n+1's prompt is the user message plus the first 2n response
		// messages (TS step 1, 2, 3 inline snapshots and the step 5
		// snapshot).
		wantPrompt := append([]interface{}{user}, want[:2*n]...)
		r71genCheckJSON(t, fmt.Sprintf("doGenerateCalls[%d].prompt", n), r71genJSON(t, call.Prompt.Messages), wantPrompt)
		if call.Prompt.System != "" {
			t.Errorf("step %d system = %q", n, call.Prompt.System)
		}
	}
	// should send correct tools in step 1
	tools := calls[0].Tools
	if len(tools) != 2 || tools[0].Name != "code_execution" || !tools[0].ProviderExecuted || tools[1].Name != "rollDie" ||
		tools[1].Description != "Roll a die and return the result." || !reflect.DeepEqual(tools[1].ProviderOptions, rollDieOptions) {
		t.Errorf("step 1 tools = %+v", tools)
	}
	// should forward container ID via providerOptions in step 2 (not in step 1)
	if calls[0].ProviderOptions != nil {
		t.Errorf("step 1 providerOptions = %v, want none", calls[0].ProviderOptions)
	}
	// Every later step forwards the previous step's container (steps 1-4
	// all carry it).
	wantOpts := r71genObj{"anthropic": r71genObj{"container": r71genObj{"id": r71genContainerID}}}
	for n := 1; n < 5; n++ {
		if !reflect.DeepEqual(calls[n].ProviderOptions, wantOpts) {
			t.Errorf("step %d providerOptions = %v, want %v", n+1, calls[n].ProviderOptions, wantOpts)
		}
	}

	// result.responseMessages
	r71genCheckJSON(t, "result.responseMessages", r71genJSON(t, res.ResponseMessages), want)

	// result.toolCalls and result.toolResults
	var callIDs, resultIDs []string
	for _, c := range res.ToolCalls {
		callIDs = append(callIDs, c.ID)
	}
	for _, r := range res.ToolResults {
		resultIDs = append(resultIDs, r.ToolCallID)
	}
	wantCallIDs := []string{r71genCodeExecID}
	var wantResultIDs []string
	for _, round := range r71genRounds {
		wantCallIDs = append(wantCallIDs, round[0][0], round[1][0])
		wantResultIDs = append(wantResultIDs, round[0][0], round[1][0])
	}
	wantResultIDs = append(wantResultIDs, r71genCodeExecID)
	if !reflect.DeepEqual(callIDs, wantCallIDs) {
		t.Errorf("result.toolCalls ids = %v, want %v", callIDs, wantCallIDs)
	}
	if !reflect.DeepEqual(resultIDs, wantResultIDs) {
		t.Errorf("result.toolResults ids = %v, want %v", resultIDs, wantResultIDs)
	}
	if len(res.FinalStep.ToolCalls) != 0 {
		t.Errorf("finalStep.toolCalls = %v, want []", res.FinalStep.ToolCalls)
	}
	// should return final step toolResults from finalStep: the deferred
	// provider result, input and dynamic undefined, providerExecuted true.
	if fr := res.FinalStep.ToolResults; len(fr) != 1 || fr[0].ToolCallID != r71genCodeExecID || fr[0].ToolName != "code_execution" ||
		!fr[0].ProviderExecuted || fr[0].Input != nil || fr[0].Dynamic || fr[0].Error != nil ||
		!reflect.DeepEqual(fr[0].Result, r71genObj{"type": "code_execution_result", "stdout": r71genStdout, "stderr": "", "return_code": float64(0), "content": []interface{}{}}) {
		t.Errorf("finalStep.toolResults = %+v", fr)
	}

	// tool execution: two rollDie calls per step for steps 1-4 (TS records
	// them in call order; Go runs a step's calls concurrently without
	// ToolCallConcurrency, so each step's players are compared sorted).
	if len(rolls) != 5 || len(rolls[4]) != 0 {
		t.Fatalf("rollDie executions per step = %v", rolls)
	}
	for n, round := range r71genRounds {
		got := append([]string(nil), rolls[n]...)
		sort.Strings(got)
		wantPlayers := []string{round[0][1], round[1][1]}
		sort.Strings(wantPlayers)
		if !reflect.DeepEqual(got, wantPlayers) {
			t.Errorf("step %d rollDie players = %v, want %v", n+1, rolls[n], wantPlayers)
		}
	}

	// result.steps, result.text, result.finishReason
	wantReasons := []types.FinishReason{"tool-calls", "tool-calls", "tool-calls", "tool-calls", "stop"}
	if len(res.Steps) != 5 {
		t.Fatalf("steps = %d, want 5", len(res.Steps))
	}
	for n, r := range wantReasons {
		if res.Steps[n].FinishReason != r {
			t.Errorf("steps[%d].finishReason = %q, want %q", n, res.Steps[n].FinishReason, r)
		}
	}
	if res.Text != r71genFinalText || res.FinishReason != types.FinishReasonStop {
		t.Errorf("text = %q, finishReason = %q", res.Text, res.FinishReason)
	}

	// result.totalUsage / onFinish usage
	wantUsage := r71genObj{"inputTokens": float64(7612), "inputTokenDetails": r71genObj{"noCacheTokens": float64(7612)},
		"outputTokens": float64(806), "outputTokenDetails": r71genObj{"textTokens": float64(806)}, "totalTokens": float64(8418)}
	r71genCheckJSON(t, "result.totalUsage", r71genJSON(t, res.Usage), wantUsage)
	r71genCheckJSON(t, "result.TotalUsage", r71genJSON(t, res.TotalUsage), wantUsage) //nolint:staticcheck // deprecated alias of Usage

	// prepareStep calls
	wantPrepare := []prepareCall{{"mock-model-id", 0, 0, 1}, {"mock-model-id", 1, 1, 3}, {"mock-model-id", 2, 2, 5}, {"mock-model-id", 3, 3, 7}, {"mock-model-id", 4, 4, 9}}
	if !reflect.DeepEqual(prepareCalls, wantPrepare) {
		t.Errorf("prepareStep calls = %+v, want %+v", prepareCalls, wantPrepare)
	}

	// onStepFinish callback
	if len(stepEvents) != 5 {
		t.Fatalf("onStepFinish calls = %d, want 5", len(stepEvents))
	}
	for n, r := range wantReasons {
		if stepEvents[n].FinishReason != r {
			t.Errorf("onStepFinish[%d].finishReason = %q, want %q", n, stepEvents[n].FinishReason, r)
		}
	}
	for n := 0; n < 2; n++ {
		r71genCheckJSON(t, "onStepFinish.providerMetadata", r71genJSON(t, stepEvents[n].ProviderMetadata),
			r71genObj{"anthropic": r71genObj{"container": r71genObj{"id": r71genContainerID}}})
	}

	// onFinish callback
	if endEvent.Text != r71genFinalText || endEvent.FinishReason != types.FinishReasonStop || len(endEvent.Steps) != 5 || len(endEvent.ResponseMessages) != 9 {
		t.Errorf("onFinish text=%q finishReason=%q steps=%d responseMessages=%d", endEvent.Text, endEvent.FinishReason, len(endEvent.Steps), len(endEvent.ResponseMessages))
	}
	r71genCheckJSON(t, "onFinish.usage", r71genJSON(t, endEvent.Usage), wantUsage)
}
