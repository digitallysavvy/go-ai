package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r74genIntPtr(v int) *int { return &v }

// TS executeTools fires onToolExecutionStart in call order for the calls that
// execute. A call between them that never reaches its start (here a client
// tool without Execute, and a provider-executed call) must not let a later
// call start before an earlier one: the chain is transitive.
func TestR74Gen_ParallelToolStartOrderSkipsCallsWithoutStart(t *testing.T) {
	exec := types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	client := types.Tool{Name: "client", Parameters: map[string]interface{}{"type": "object"}}
	provider := types.Tool{Name: "web", Parameters: map[string]interface{}{"type": "object"}, ProviderExecuted: true}
	for _, concurrency := range []*int{nil, r74genIntPtr(4), r74genIntPtr(2)} {
		var mu sync.Mutex
		var starts []string
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: r72genSeqModel(t,
				r72genCalls(
					types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}},
					types.ToolCall{ID: "call-2", ToolName: "client", Arguments: map[string]interface{}{}},
					types.ToolCall{ID: "call-3", ToolName: "web", Arguments: map[string]interface{}{}, ProviderExecuted: true},
					types.ToolCall{ID: "call-4", ToolName: "tool1", Arguments: map[string]interface{}{}}),
				r72genText("done")),
			Prompt:              "go",
			Tools:               []types.Tool{exec, client, provider},
			ToolCallConcurrency: concurrency,
			OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) {
				if e.ToolCall.ID == "call-1" {
					time.Sleep(40 * time.Millisecond)
				}
				mu.Lock()
				defer mu.Unlock()
				starts = append(starts, e.ToolCall.ID)
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		got := strings.Join(starts, ",")
		mu.Unlock()
		if got != "call-1,call-4" {
			t.Errorf("concurrency=%v: start order = %s, want call-1,call-4", concurrency, got)
		}
	}
}

func r74genExecTool() types.Tool {
	return types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
}

// StreamText shares executeToolsLimited: same transitive start order.
func TestR74Gen_StreamParallelToolStartOrderSkipsCallsWithoutStart(t *testing.T) {
	n := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			n++
			if n == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-2", ToolName: "client", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-3", ToolName: "tool1", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var mu sync.Mutex
	var starts []string
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "go",
		Tools:  []types.Tool{r74genExecTool(), {Name: "client", Parameters: map[string]interface{}{"type": "object"}}},
		OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) {
			if e.ToolCall.ID == "call-1" {
				time.Sleep(40 * time.Millisecond)
			}
			mu.Lock()
			defer mu.Unlock()
			starts = append(starts, e.ToolCall.ID)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(starts, ","); got != "call-1,call-3" {
		t.Fatalf("start order = %s, want call-1,call-3", got)
	}
}

// A start callback that panics still releases the next call (Notify recovers
// the panic and the gate is closed after it), and both calls execute.
func TestR74Gen_ParallelToolStartGatePanickingCallback(t *testing.T) {
	var mu sync.Mutex
	var starts []string
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t,
			r72genCalls(
				types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}},
				types.ToolCall{ID: "call-2", ToolName: "tool1", Arguments: map[string]interface{}{}}),
			r72genText("done")),
		Prompt:   "go",
		Tools:    []types.Tool{r74genExecTool()},
		StopWhen: []StopCondition{StepCountIs(2)},
		OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) {
			mu.Lock()
			starts = append(starts, e.ToolCall.ID)
			mu.Unlock()
			if e.ToolCall.ID == "call-1" {
				panic("boom")
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(starts, ","); got != "call-1,call-2" {
		t.Fatalf("start order = %s, want call-1,call-2", got)
	}
	if len(res.Steps) != 2 || len(res.Steps[0].ToolResults) != 2 {
		t.Fatalf("steps = %d, first step results = %+v", len(res.Steps), res.Steps[0].ToolResults)
	}
}

// A start callback that blocks until the run is cancelled must not leave the
// later call waiting on its gate forever: the gate wait ends with ctx.
func TestR74Gen_ParallelToolStartGateCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	call2Started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := GenerateText(ctx, GenerateTextOptions{
			Model: r72genSeqModel(t,
				r72genCalls(
					types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}},
					types.ToolCall{ID: "call-2", ToolName: "tool1", Arguments: map[string]interface{}{}}),
				r72genText("done")),
			Prompt: "go",
			Tools:  []types.Tool{r74genExecTool()},
			OnToolExecutionStart: func(cbCtx context.Context, e OnToolCallStartEvent) {
				if e.ToolCall.ID == "call-1" {
					<-ctx.Done()
					return
				}
				close(call2Started)
			},
		})
		done <- err
	}()
	select {
	case <-call2Started:
		t.Fatal("call-2 started before call-1 fired its start")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		// The step ends after the tools ran (default StepCountIs(1)); the
		// point is that the run returns at all.
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GenerateText did not return after cancellation")
	}
}

// ---------------------------------------------------------------------------
// generate-text.test.ts 'programmatic tool calling' > '5 steps: code_execution
// triggers client tool across multiple turns (dice game fixture)' (TS
// 10190-11054). One GenerateText run replays the fixture; each TS case is a
// subtest with the TS assertion.
// ---------------------------------------------------------------------------

const (
	r74genDiceCodeExecID  = "srvtoolu_01CberhXc9TgYXrCZU8bQoks"
	r74genDiceContainerID = "container_011CWHQB57xVregfCMPrKgew"
	r74genDiceIntro       = "I'll help you simulate this dice game between two players! Let me run the game where both players roll dice each round until one player wins 3 rounds."
	r74genDiceStdout      = "============================================================\nDICE GAME: First to 3 Round Wins\n============================================================\n\nRound 1:\n  Player 1 rolls: 6\n  Player 2 rolls: 6\n  → Draw! No points awarded.\n  Score: Player 1: 0 | Player 2: 0\n\nRound 2:\n  Player 1 rolls: 5\n  Player 2 rolls: 4\n  → Player 1 wins this round!\n  Score: Player 1: 1 | Player 2: 0\n\nRound 3:\n  Player 1 rolls: 6\n  Player 2 rolls: 4\n  → Player 1 wins this round!\n  Score: Player 1: 2 | Player 2: 0\n\nRound 4:\n  Player 1 rolls: 6\n  Player 2 rolls: 3\n  → Player 1 wins this round!\n  Score: Player 1: 3 | Player 2: 0\n\n============================================================\n🏆 PLAYER 1 WINS THE GAME!\nFinal Score: Player 1: 3 | Player 2: 0\nTotal Rounds: 4\n============================================================\n"
	r74genDiceFinalText   = "**Game Over!**\n\nPlayer 1 dominated this game with a decisive 3-0 victory! Looking at the rolls:\n- **Round 1**: Both rolled 6 (Draw)\n- **Round 2**: Player 1 (5) beat Player 2 (4)\n- **Round 3**: Player 1 (6) beat Player 2 (4)\n- **Round 4**: Player 1 (6) beat Player 2 (3)\n\nBased on these results, it appears **Player 1 is likely the one with the loaded die** - they rolled 6 three times out of four rolls (including the draw), and consistently rolled high numbers (5, 6, 6, 6). Player 2's rolls were more varied and lower (6, 4, 4, 3), which looks more like a fair die distribution.\n\nThe loaded die gave Player 1 a significant advantage, allowing them to win the game without Player 2 scoring a single round!"
)

func r74genDiceUsage(in, out int64) types.Usage {
	return types.Usage{
		InputTokens:   &in,
		InputDetails:  &types.InputTokenDetails{NoCacheTokens: &in},
		OutputTokens:  &out,
		OutputDetails: &types.OutputTokenDetails{TextTokens: &out},
	}
}

func r74genDiceCall(id, input string, providerExecuted bool, name string) (types.ToolCall, types.ToolCallContent) {
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(input), &args)
	return types.ToolCall{ID: id, ToolName: name, Arguments: args, RawArguments: input, ProviderExecuted: providerExecuted},
		types.ToolCallContent{ToolCallID: id, ToolName: name, Input: input, ProviderExecuted: providerExecuted}
}

func r74genDiceStep(i int, text string, calls [][3]string, u types.Usage, id string, meta bool) *types.GenerateResult {
	r := &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, Usage: u,
		ResponseMetadata: &types.ResponseMetadata{ID: id, Timestamp: time.UnixMilli(int64(i) * 1000).UTC(), ModelID: "claude-sonnet-4-5-20250929"}}
	if text != "" {
		r.Content = append(r.Content, types.TextContent{Text: text})
	}
	for _, c := range calls {
		call, part := r74genDiceCall(c[0], c[2], c[1] == "code_execution", c[1])
		r.ToolCalls = append(r.ToolCalls, call)
		r.Content = append(r.Content, part)
	}
	if meta {
		r.ProviderMetadata = map[string]interface{}{"anthropic": map[string]interface{}{"container": map[string]interface{}{"id": r74genDiceContainerID}}}
	}
	return r
}

type r74genDiceRun struct {
	res             *GenerateTextResult
	finish          *GenerateTextResult
	stepFinishes    []types.StepResult
	calls           []*provider.GenerateOptions
	prepareSteps    []PrepareStepOptions
	rollsByCallID   map[string]string
	rollCallIDOrder []string
}

func r74genRunDice(t *testing.T) *r74genDiceRun {
	t.Helper()
	run := &r74genDiceRun{rollsByCallID: map[string]string{}}
	var mu sync.Mutex
	steps := []*types.GenerateResult{
		r74genDiceStep(0, r74genDiceIntro, [][3]string{
			{r74genDiceCodeExecID, "code_execution", `{"type":"programmatic-tool-call","code":"game_loop()"}`},
			{"toolu_01PMcE1JBKCeLjn83cgUCvR5", "rollDie", `{ "player": "player2" }`},
			{"toolu_01MZf5QJ1EQyd2yGyeLzBxAS", "rollDie", `{ "player": "player1" }`},
		}, r74genDiceUsage(3369, 577), "msg_01V3gktwqnMAsXxpJ6sG9KDR", true),
		r74genDiceStep(1, "", [][3]string{
			{"toolu_01UvVQ2xwA6preZppeajCkYK", "rollDie", `{ "player": "player1" }`},
			{"toolu_01BghspNownQFtRgv8jVicr3", "rollDie", `{ "player": "player2" }`},
		}, r74genDiceUsage(0, 0), "msg_01DmEmveJkWRUR1y41DfGfEQ", true),
		r74genDiceStep(2, "", [][3]string{
			{"toolu_01T7Upuuv8C71nq7DZ9ZPNQW", "rollDie", `{ "player": "player1" }`},
			{"toolu_016Da1tDet9Bf7dAdYTkF5Ar", "rollDie", `{ "player": "player2" }`},
		}, r74genDiceUsage(0, 0), "msg_01AxicwpZwTqKCtty95VTAoL", true),
		r74genDiceStep(3, "", [][3]string{
			{"toolu_01DiUBRds64sNajVPTZRrDSM", "rollDie", `{ "player": "player1" }`},
			{"toolu_01XQa3r3y1Fe8rnkGSncq626", "rollDie", `{ "player": "player2" }`},
		}, r74genDiceUsage(0, 0), "msg_01TjXmSMrfqKVMpHHM3wMaBy", true),
		{
			Content: []types.ContentPart{
				types.ToolResultContent{ToolCallID: r74genDiceCodeExecID, ToolName: "code_execution", ProviderExecuted: true,
					Result: map[string]interface{}{"type": "code_execution_result", "stdout": r74genDiceStdout, "stderr": "", "return_code": 0, "content": []interface{}{}}},
				types.TextContent{Text: r74genDiceFinalText},
			},
			FinishReason:     types.FinishReasonStop,
			Usage:            r74genDiceUsage(4243, 229),
			ResponseMetadata: &types.ResponseMetadata{ID: "msg_01LPPbjvqcSgBPfApMEiJ4qv", Timestamp: time.UnixMilli(4000).UTC(), ModelID: "claude-sonnet-4-5-20250929"},
		},
	}
	n := 0
	model := &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			defer mu.Unlock()
			run.calls = append(run.calls, o)
			if n >= len(steps) {
				return nil, errors.New("unexpected model call")
			}
			r := *steps[n]
			n++
			return &r, nil
		},
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Play a dice game between two players.",
		Tools: []types.Tool{
			{Name: "code_execution", Type: "provider", ProviderID: "anthropic.code_execution_20250825", ProviderArgs: map[string]interface{}{},
				ProviderExecuted: true, SupportsDeferredResults: true,
				Parameters:   map[string]interface{}{"type": "object", "properties": map[string]interface{}{"code": map[string]interface{}{"type": "string"}}, "required": []interface{}{"code"}},
				OutputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"stdout": map[string]interface{}{"type": "string"}, "stderr": map[string]interface{}{"type": "string"}}, "required": []interface{}{"stdout", "stderr"}}},
			{Name: "rollDie", Description: "Roll a die and return the result.",
				Parameters: map[string]interface{}{"type": "object", "additionalProperties": false, "required": []interface{}{"player"},
					"properties": map[string]interface{}{"player": map[string]interface{}{"type": "string", "enum": []interface{}{"player1", "player2"}}}},
				ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"allowedCallers": []interface{}{"code_execution_20250825"}}},
				Execute: func(_ context.Context, in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
					p, _ := in["player"].(string)
					mu.Lock()
					run.rollsByCallID[o.ToolCallID] = p
					mu.Unlock()
					if p == "player1" {
						return 6, nil
					}
					return 3, nil
				}},
		},
		StopWhen: []StopCondition{IsStepCount(10)},
		OnFinish: func(_ context.Context, r *GenerateTextResult, _ interface{}) { run.finish = r },
		OnStepFinish: func(_ context.Context, s types.StepResult, _ interface{}) {
			run.stepFinishes = append(run.stepFinishes, s)
		},
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			snapshot := s
			snapshot.Steps = append([]types.StepResult(nil), s.Steps...)
			snapshot.Messages = append([]types.Message(nil), s.Messages...)
			run.prepareSteps = append(run.prepareSteps, snapshot)
			if s.StepNumber > 0 && len(s.Steps) > 0 {
				last := s.Steps[len(s.Steps)-1]
				if a, ok := last.ProviderMetadata["anthropic"].(map[string]interface{}); ok {
					if c, ok := a["container"].(map[string]interface{}); ok && c["id"] != nil {
						s.ProviderOptions = map[string]interface{}{"anthropic": map[string]interface{}{"container": map[string]interface{}{"id": c["id"]}}}
					}
				}
			}
			return s
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	run.res = res
	for _, step := range res.Steps {
		for _, c := range step.ToolCalls {
			if c.ToolName == "rollDie" {
				run.rollCallIDOrder = append(run.rollCallIDOrder, c.ID)
			}
		}
	}
	return run
}

// r74genDiceMessages renders messages in the TS ModelMessage shape. TS zod
// strips the undeclared "type" key from the code_execution input; Go keeps it
// (known difference "Tool call input keeps undeclared keys"), so it is
// removed before comparing.
func r74genDiceMessages(t *testing.T, msgs []types.Message) interface{} {
	t.Helper()
	got := r72genTSMessages(t, msgs)
	for _, m := range got.([]interface{}) {
		for _, p := range m.(map[string]interface{})["content"].([]interface{}) {
			part := p.(map[string]interface{})
			if part["toolName"] == "code_execution" && part["type"] == "tool-call" {
				if in, ok := part["input"].(map[string]interface{}); ok {
					delete(in, "type")
				}
			}
		}
	}
	return got
}

func r74genDiceCheckJSON(t *testing.T, got interface{}, wantJSON string) {
	t.Helper()
	var want interface{}
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		gj, _ := json.Marshal(got)
		t.Fatalf("got\n%s\nwant\n%s", gj, wantJSON)
	}
}

func r74genUsageJSON(t *testing.T, u types.Usage) interface{} {
	t.Helper()
	return r72genJSON(t, u)
}

func TestR74Gen_ProgrammaticDiceGameGenerateText(t *testing.T) {
	run := r74genRunDice(t)
	res := run.res
	wantContainer := map[string]interface{}{"anthropic": map[string]interface{}{"container": map[string]interface{}{"id": r74genDiceContainerID}}}

	// step inputs
	t.Run("should send correct prompt in step 1", func(t *testing.T) {
		r74genDiceCheckJSON(t, r74genDiceMessages(t, run.calls[0].Prompt.Messages),
			`[{"content":[{"text":"Play a dice game between two players.","type":"text"}],"role":"user"}]`)
	})
	t.Run("should send correct tools in step 1", func(t *testing.T) {
		tools := run.calls[0].Tools
		if len(tools) != 2 {
			t.Fatalf("tools = %d, want 2", len(tools))
		}
		ce, rd := tools[0], tools[1]
		if ce.Name != "code_execution" || ce.Type != "provider" || ce.ProviderID != "anthropic.code_execution_20250825" || ce.ProviderArgs == nil || len(ce.ProviderArgs) != 0 {
			t.Fatalf("tool 0 = %+v, want provider anthropic.code_execution_20250825 with args {}", ce)
		}
		if rd.Name != "rollDie" || rd.Description != "Roll a die and return the result." || rd.Type == "provider" {
			t.Fatalf("tool 1 = %+v", rd)
		}
		if !reflect.DeepEqual(r72genJSON(t, rd.ProviderOptions), r72genJSON(t, map[string]interface{}{"anthropic": map[string]interface{}{"allowedCallers": []string{"code_execution_20250825"}}})) {
			t.Fatalf("rollDie providerOptions = %v", rd.ProviderOptions)
		}
		params := r72genJSON(t, rd.Parameters).(map[string]interface{})
		if !reflect.DeepEqual(params["properties"], map[string]interface{}{"player": map[string]interface{}{"type": "string", "enum": []interface{}{"player1", "player2"}}}) ||
			!reflect.DeepEqual(params["required"], []interface{}{"player"}) || params["additionalProperties"] != false {
			t.Fatalf("rollDie inputSchema = %v", params)
		}
	})
	t.Run("should include assistant messages and tool results in step 2 prompt", func(t *testing.T) {
		r74genDiceCheckJSON(t, r74genDiceMessages(t, run.calls[1].Prompt.Messages), r74genDiceStep2Prompt)
	})
	t.Run("should include all previous messages in step 3 prompt (round 3)", func(t *testing.T) {
		r74genDiceCheckJSON(t, r74genDiceMessages(t, run.calls[2].Prompt.Messages), r74genDiceStep3Prompt)
	})
	t.Run("should forward container ID via providerOptions in step 2", func(t *testing.T) {
		if !reflect.DeepEqual(r72genJSON(t, run.calls[1].ProviderOptions), wantContainer) {
			t.Fatalf("step 2 providerOptions = %v", run.calls[1].ProviderOptions)
		}
	})
	t.Run("should include all previous messages in step 5 prompt (final step)", func(t *testing.T) {
		r74genDiceCheckJSON(t, r74genDiceMessages(t, run.calls[4].Prompt.Messages), r74genDiceStep5Prompt)
	})

	// result.responseMessages
	t.Run("should contain response messages from all steps", func(t *testing.T) {
		r74genDiceCheckJSON(t, r74genDiceMessages(t, res.ResponseMessages), r74genDiceResponseMessages)
	})

	// result.toolCalls and result.toolResults
	t.Run("should return toolCalls from all steps", func(t *testing.T) {
		var ids []string
		for _, c := range res.ToolCalls {
			ids = append(ids, c.ID)
		}
		want := []string{r74genDiceCodeExecID, "toolu_01PMcE1JBKCeLjn83cgUCvR5", "toolu_01MZf5QJ1EQyd2yGyeLzBxAS", "toolu_01UvVQ2xwA6preZppeajCkYK", "toolu_01BghspNownQFtRgv8jVicr3",
			"toolu_01T7Upuuv8C71nq7DZ9ZPNQW", "toolu_016Da1tDet9Bf7dAdYTkF5Ar", "toolu_01DiUBRds64sNajVPTZRrDSM", "toolu_01XQa3r3y1Fe8rnkGSncq626"}
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("toolCalls = %v, want %v", ids, want)
		}
	})
	t.Run("should return empty finalStep.toolCalls when final step has no tool calls", func(t *testing.T) {
		if len(res.FinalStep.ToolCalls) != 0 {
			t.Fatalf("finalStep.toolCalls = %v", res.FinalStep.ToolCalls)
		}
	})
	t.Run("should return toolResults from all steps", func(t *testing.T) {
		var ids []string
		for _, r := range res.ToolResults {
			ids = append(ids, r.ToolCallID)
		}
		want := []string{"toolu_01PMcE1JBKCeLjn83cgUCvR5", "toolu_01MZf5QJ1EQyd2yGyeLzBxAS", "toolu_01UvVQ2xwA6preZppeajCkYK", "toolu_01BghspNownQFtRgv8jVicr3",
			"toolu_01T7Upuuv8C71nq7DZ9ZPNQW", "toolu_016Da1tDet9Bf7dAdYTkF5Ar", "toolu_01DiUBRds64sNajVPTZRrDSM", "toolu_01XQa3r3y1Fe8rnkGSncq626", r74genDiceCodeExecID}
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("toolResults = %v, want %v", ids, want)
		}
	})
	t.Run("should return final step toolResults from finalStep", func(t *testing.T) {
		var got []interface{}
		for _, r := range res.FinalStep.ToolResults {
			part := map[string]interface{}{"type": "tool-result", "toolCallId": r.ToolCallID, "toolName": r.ToolName, "output": r.Result}
			if r.ProviderExecuted {
				part["providerExecuted"] = true
			}
			if r.Input != nil {
				part["input"] = r.Input
			}
			if r.Dynamic {
				part["dynamic"] = true
			}
			got = append(got, part)
		}
		r74genDiceCheckJSON(t, r72genJSON(t, got), r74genDiceFinalToolResults)
	})

	// tool execution
	t.Run("should execute rollDie tool 4 times (twice per step for steps 1 and 2)", func(t *testing.T) {
		// Go runs a step's tool calls concurrently, so executions are read
		// back in call order.
		var got []string
		for _, id := range run.rollCallIDOrder {
			got = append(got, run.rollsByCallID[id])
		}
		want := []string{"player2", "player1", "player1", "player2", "player1", "player2", "player1", "player2"}
		if !reflect.DeepEqual(got, want) || len(run.rollsByCallID) != 8 {
			t.Fatalf("rollDie executions = %v (%d), want %v", got, len(run.rollsByCallID), want)
		}
	})

	// result.steps, result.text, result.finishReason
	wantReasons := []types.FinishReason{"tool-calls", "tool-calls", "tool-calls", "tool-calls", "stop"}
	t.Run("should contain 5 steps", func(t *testing.T) {
		if len(res.Steps) != 5 {
			t.Fatalf("steps = %d", len(res.Steps))
		}
	})
	t.Run("should have correct finishReason for each step", func(t *testing.T) {
		for i, want := range wantReasons {
			if res.Steps[i].FinishReason != want {
				t.Fatalf("step %d finishReason = %v, want %v", i, res.Steps[i].FinishReason, want)
			}
		}
	})
	t.Run("should return final text from last step", func(t *testing.T) {
		if !strings.Contains(res.Text, "**Game Over!**") {
			t.Fatalf("text = %q", res.Text)
		}
	})
	t.Run("should return stop from final step", func(t *testing.T) {
		if res.FinishReason != types.FinishReasonStop {
			t.Fatalf("finishReason = %v", res.FinishReason)
		}
	})

	wantUsage := `{"inputTokenDetails":{"noCacheTokens":7612},"inputTokens":7612,"outputTokenDetails":{"textTokens":806},"outputTokens":806,"totalTokens":8418}`
	t.Run("should sum token usage across all steps", func(t *testing.T) {
		r74genDiceCheckJSON(t, r74genUsageJSON(t, res.TotalUsage), wantUsage)
	})

	// prepareStep calls
	t.Run("should call prepareStep for each step with correct stepNumber", func(t *testing.T) {
		if len(run.prepareSteps) != 5 {
			t.Fatalf("prepareStep calls = %d", len(run.prepareSteps))
		}
		for i, p := range run.prepareSteps {
			if p.StepNumber != i {
				t.Fatalf("call %d stepNumber = %d", i, p.StepNumber)
			}
		}
	})
	t.Run("should pass empty steps array for first prepareStep call", func(t *testing.T) {
		if len(run.prepareSteps[0].Steps) != 0 {
			t.Fatalf("steps = %d", len(run.prepareSteps[0].Steps))
		}
	})
	t.Run("should pass accumulated steps to subsequent prepareStep calls", func(t *testing.T) {
		for i := 1; i < 5; i++ {
			if len(run.prepareSteps[i].Steps) != i {
				t.Fatalf("call %d steps = %d", i, len(run.prepareSteps[i].Steps))
			}
		}
	})
	t.Run("should pass accumulated messages to prepareStep", func(t *testing.T) {
		for i, want := range []int{1, 3, 5, 7, 9} {
			if got := len(run.prepareSteps[i].Messages); got != want {
				t.Fatalf("call %d messages = %d, want %d", i, got, want)
			}
		}
	})

	// onStepFinish callback
	t.Run("should be called for each step", func(t *testing.T) {
		if len(run.stepFinishes) != 5 {
			t.Fatalf("onStepFinish calls = %d", len(run.stepFinishes))
		}
	})
	t.Run("should contain correct finishReason for each step", func(t *testing.T) {
		for i, want := range wantReasons {
			if run.stepFinishes[i].FinishReason != want {
				t.Fatalf("step %d finishReason = %v", i, run.stepFinishes[i].FinishReason)
			}
		}
	})
	t.Run("should contain provider metadata with container ID for steps 1 and 2", func(t *testing.T) {
		for i := 0; i < 2; i++ {
			if !reflect.DeepEqual(r72genJSON(t, run.stepFinishes[i].ProviderMetadata), wantContainer) {
				t.Fatalf("step %d providerMetadata = %v", i, run.stepFinishes[i].ProviderMetadata)
			}
		}
	})

	// onFinish callback
	t.Run("onFinish should be called with correct text", func(t *testing.T) {
		if run.finish == nil || !strings.Contains(run.finish.Text, "**Game Over!**") {
			t.Fatalf("onFinish = %+v", run.finish)
		}
	})
	t.Run("onFinish should be called with correct finishReason", func(t *testing.T) {
		if run.finish.FinishReason != types.FinishReasonStop {
			t.Fatalf("finishReason = %v", run.finish.FinishReason)
		}
	})
	t.Run("onFinish should contain all steps", func(t *testing.T) {
		if len(run.finish.Steps) != 5 {
			t.Fatalf("steps = %d", len(run.finish.Steps))
		}
	})
	t.Run("onFinish should contain correct usage", func(t *testing.T) {
		// TS onEnd `usage` is the total usage.
		r74genDiceCheckJSON(t, r74genUsageJSON(t, run.finish.Usage), wantUsage)
	})
	t.Run("onFinish should contain response messages from all steps", func(t *testing.T) {
		if len(run.finish.ResponseMessages) != 9 {
			t.Fatalf("responseMessages = %d", len(run.finish.ResponseMessages))
		}
	})
}

// TS snapshots of the generate-text.test.ts dice game fixture (inline
// snapshots and __snapshots__/generate-text.test.ts.snap), as JSON with the
// `undefined` fields dropped.
const (
	r74genDiceStep2Prompt      = `[{"content":[{"text":"Play a dice game between two players.","type":"text"}],"role":"user"},{"content":[{"text":"I'll help you simulate this dice game between two players! Let me run the game where both players roll dice each round until one player wins 3 rounds.","type":"text"},{"input":{"code":"game_loop()"},"providerExecuted":true,"toolCallId":"srvtoolu_01CberhXc9TgYXrCZU8bQoks","toolName":"code_execution","type":"tool-call"},{"input":{"player":"player2"},"toolCallId":"toolu_01PMcE1JBKCeLjn83cgUCvR5","toolName":"rollDie","type":"tool-call"},{"input":{"player":"player1"},"toolCallId":"toolu_01MZf5QJ1EQyd2yGyeLzBxAS","toolName":"rollDie","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"json","value":3},"toolCallId":"toolu_01PMcE1JBKCeLjn83cgUCvR5","toolName":"rollDie","type":"tool-result"},{"output":{"type":"json","value":6},"toolCallId":"toolu_01MZf5QJ1EQyd2yGyeLzBxAS","toolName":"rollDie","type":"tool-result"}],"role":"tool"}]`
	r74genDiceStep3Prompt      = `[{"content":[{"text":"Play a dice game between two players.","type":"text"}],"role":"user"},{"content":[{"text":"I'll help you simulate this dice game between two players! Let me run the game where both players roll dice each round until one player wins 3 rounds.","type":"text"},{"input":{"code":"game_loop()"},"providerExecuted":true,"toolCallId":"srvtoolu_01CberhXc9TgYXrCZU8bQoks","toolName":"code_execution","type":"tool-call"},{"input":{"player":"player2"},"toolCallId":"toolu_01PMcE1JBKCeLjn83cgUCvR5","toolName":"rollDie","type":"tool-call"},{"input":{"player":"player1"},"toolCallId":"toolu_01MZf5QJ1EQyd2yGyeLzBxAS","toolName":"rollDie","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"json","value":3},"toolCallId":"toolu_01PMcE1JBKCeLjn83cgUCvR5","toolName":"rollDie","type":"tool-result"},{"output":{"type":"json","value":6},"toolCallId":"toolu_01MZf5QJ1EQyd2yGyeLzBxAS","toolName":"rollDie","type":"tool-result"}],"role":"tool"},{"content":[{"input":{"player":"player1"},"toolCallId":"toolu_01UvVQ2xwA6preZppeajCkYK","toolName":"rollDie","type":"tool-call"},{"input":{"player":"player2"},"toolCallId":"toolu_01BghspNownQFtRgv8jVicr3","toolName":"rollDie","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"json","value":6},"toolCallId":"toolu_01UvVQ2xwA6preZppeajCkYK","toolName":"rollDie","type":"tool-result"},{"output":{"type":"json","value":3},"toolCallId":"toolu_01BghspNownQFtRgv8jVicr3","toolName":"rollDie","type":"tool-result"}],"role":"tool"}]`
	r74genDiceStep5Prompt      = `[{"content":[{"text":"Play a dice game between two players.","type":"text"}],"role":"user"},{"content":[{"text":"I'll help you simulate this dice game between two players! Let me run the game where both players roll dice each round until one player wins 3 rounds.","type":"text"},{"input":{"code":"game_loop()"},"providerExecuted":true,"toolCallId":"srvtoolu_01CberhXc9TgYXrCZU8bQoks","toolName":"code_execution","type":"tool-call"},{"input":{"player":"player2"},"toolCallId":"toolu_01PMcE1JBKCeLjn83cgUCvR5","toolName":"rollDie","type":"tool-call"},{"input":{"player":"player1"},"toolCallId":"toolu_01MZf5QJ1EQyd2yGyeLzBxAS","toolName":"rollDie","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"json","value":3},"toolCallId":"toolu_01PMcE1JBKCeLjn83cgUCvR5","toolName":"rollDie","type":"tool-result"},{"output":{"type":"json","value":6},"toolCallId":"toolu_01MZf5QJ1EQyd2yGyeLzBxAS","toolName":"rollDie","type":"tool-result"}],"role":"tool"},{"content":[{"input":{"player":"player1"},"toolCallId":"toolu_01UvVQ2xwA6preZppeajCkYK","toolName":"rollDie","type":"tool-call"},{"input":{"player":"player2"},"toolCallId":"toolu_01BghspNownQFtRgv8jVicr3","toolName":"rollDie","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"json","value":6},"toolCallId":"toolu_01UvVQ2xwA6preZppeajCkYK","toolName":"rollDie","type":"tool-result"},{"output":{"type":"json","value":3},"toolCallId":"toolu_01BghspNownQFtRgv8jVicr3","toolName":"rollDie","type":"tool-result"}],"role":"tool"},{"content":[{"input":{"player":"player1"},"toolCallId":"toolu_01T7Upuuv8C71nq7DZ9ZPNQW","toolName":"rollDie","type":"tool-call"},{"input":{"player":"player2"},"toolCallId":"toolu_016Da1tDet9Bf7dAdYTkF5Ar","toolName":"rollDie","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"json","value":6},"toolCallId":"toolu_01T7Upuuv8C71nq7DZ9ZPNQW","toolName":"rollDie","type":"tool-result"},{"output":{"type":"json","value":3},"toolCallId":"toolu_016Da1tDet9Bf7dAdYTkF5Ar","toolName":"rollDie","type":"tool-result"}],"role":"tool"},{"content":[{"input":{"player":"player1"},"toolCallId":"toolu_01DiUBRds64sNajVPTZRrDSM","toolName":"rollDie","type":"tool-call"},{"input":{"player":"player2"},"toolCallId":"toolu_01XQa3r3y1Fe8rnkGSncq626","toolName":"rollDie","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"json","value":6},"toolCallId":"toolu_01DiUBRds64sNajVPTZRrDSM","toolName":"rollDie","type":"tool-result"},{"output":{"type":"json","value":3},"toolCallId":"toolu_01XQa3r3y1Fe8rnkGSncq626","toolName":"rollDie","type":"tool-result"}],"role":"tool"}]`
	r74genDiceResponseMessages = `[{"content":[{"text":"I'll help you simulate this dice game between two players! Let me run the game where both players roll dice each round until one player wins 3 rounds.","type":"text"},{"input":{"code":"game_loop()"},"providerExecuted":true,"toolCallId":"srvtoolu_01CberhXc9TgYXrCZU8bQoks","toolName":"code_execution","type":"tool-call"},{"input":{"player":"player2"},"toolCallId":"toolu_01PMcE1JBKCeLjn83cgUCvR5","toolName":"rollDie","type":"tool-call"},{"input":{"player":"player1"},"toolCallId":"toolu_01MZf5QJ1EQyd2yGyeLzBxAS","toolName":"rollDie","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"json","value":3},"toolCallId":"toolu_01PMcE1JBKCeLjn83cgUCvR5","toolName":"rollDie","type":"tool-result"},{"output":{"type":"json","value":6},"toolCallId":"toolu_01MZf5QJ1EQyd2yGyeLzBxAS","toolName":"rollDie","type":"tool-result"}],"role":"tool"},{"content":[{"input":{"player":"player1"},"toolCallId":"toolu_01UvVQ2xwA6preZppeajCkYK","toolName":"rollDie","type":"tool-call"},{"input":{"player":"player2"},"toolCallId":"toolu_01BghspNownQFtRgv8jVicr3","toolName":"rollDie","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"json","value":6},"toolCallId":"toolu_01UvVQ2xwA6preZppeajCkYK","toolName":"rollDie","type":"tool-result"},{"output":{"type":"json","value":3},"toolCallId":"toolu_01BghspNownQFtRgv8jVicr3","toolName":"rollDie","type":"tool-result"}],"role":"tool"},{"content":[{"input":{"player":"player1"},"toolCallId":"toolu_01T7Upuuv8C71nq7DZ9ZPNQW","toolName":"rollDie","type":"tool-call"},{"input":{"player":"player2"},"toolCallId":"toolu_016Da1tDet9Bf7dAdYTkF5Ar","toolName":"rollDie","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"json","value":6},"toolCallId":"toolu_01T7Upuuv8C71nq7DZ9ZPNQW","toolName":"rollDie","type":"tool-result"},{"output":{"type":"json","value":3},"toolCallId":"toolu_016Da1tDet9Bf7dAdYTkF5Ar","toolName":"rollDie","type":"tool-result"}],"role":"tool"},{"content":[{"input":{"player":"player1"},"toolCallId":"toolu_01DiUBRds64sNajVPTZRrDSM","toolName":"rollDie","type":"tool-call"},{"input":{"player":"player2"},"toolCallId":"toolu_01XQa3r3y1Fe8rnkGSncq626","toolName":"rollDie","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"json","value":6},"toolCallId":"toolu_01DiUBRds64sNajVPTZRrDSM","toolName":"rollDie","type":"tool-result"},{"output":{"type":"json","value":3},"toolCallId":"toolu_01XQa3r3y1Fe8rnkGSncq626","toolName":"rollDie","type":"tool-result"}],"role":"tool"},{"content":[{"output":{"type":"json","value":{"content":[],"return_code":0,"stderr":"","stdout":"============================================================\nDICE GAME: First to 3 Round Wins\n============================================================\n\nRound 1:\n  Player 1 rolls: 6\n  Player 2 rolls: 6\n  → Draw! No points awarded.\n  Score: Player 1: 0 | Player 2: 0\n\nRound 2:\n  Player 1 rolls: 5\n  Player 2 rolls: 4\n  → Player 1 wins this round!\n  Score: Player 1: 1 | Player 2: 0\n\nRound 3:\n  Player 1 rolls: 6\n  Player 2 rolls: 4\n  → Player 1 wins this round!\n  Score: Player 1: 2 | Player 2: 0\n\nRound 4:\n  Player 1 rolls: 6\n  Player 2 rolls: 3\n  → Player 1 wins this round!\n  Score: Player 1: 3 | Player 2: 0\n\n============================================================\n🏆 PLAYER 1 WINS THE GAME!\nFinal Score: Player 1: 3 | Player 2: 0\nTotal Rounds: 4\n============================================================\n","type":"code_execution_result"}},"toolCallId":"srvtoolu_01CberhXc9TgYXrCZU8bQoks","toolName":"code_execution","type":"tool-result"},{"text":"**Game Over!**\n\nPlayer 1 dominated this game with a decisive 3-0 victory! Looking at the rolls:\n- **Round 1**: Both rolled 6 (Draw)\n- **Round 2**: Player 1 (5) beat Player 2 (4)\n- **Round 3**: Player 1 (6) beat Player 2 (4)\n- **Round 4**: Player 1 (6) beat Player 2 (3)\n\nBased on these results, it appears **Player 1 is likely the one with the loaded die** - they rolled 6 three times out of four rolls (including the draw), and consistently rolled high numbers (5, 6, 6, 6). Player 2's rolls were more varied and lower (6, 4, 4, 3), which looks more like a fair die distribution.\n\nThe loaded die gave Player 1 a significant advantage, allowing them to win the game without Player 2 scoring a single round!","type":"text"}],"role":"assistant"}]`
	r74genDiceFinalToolResults = `[{"output":{"content":[],"return_code":0,"stderr":"","stdout":"============================================================\nDICE GAME: First to 3 Round Wins\n============================================================\n\nRound 1:\n  Player 1 rolls: 6\n  Player 2 rolls: 6\n  → Draw! No points awarded.\n  Score: Player 1: 0 | Player 2: 0\n\nRound 2:\n  Player 1 rolls: 5\n  Player 2 rolls: 4\n  → Player 1 wins this round!\n  Score: Player 1: 1 | Player 2: 0\n\nRound 3:\n  Player 1 rolls: 6\n  Player 2 rolls: 4\n  → Player 1 wins this round!\n  Score: Player 1: 2 | Player 2: 0\n\nRound 4:\n  Player 1 rolls: 6\n  Player 2 rolls: 3\n  → Player 1 wins this round!\n  Score: Player 1: 3 | Player 2: 0\n\n============================================================\n🏆 PLAYER 1 WINS THE GAME!\nFinal Score: Player 1: 3 | Player 2: 0\nTotal Rounds: 4\n============================================================\n","type":"code_execution_result"},"providerExecuted":true,"toolCallId":"srvtoolu_01CberhXc9TgYXrCZU8bQoks","toolName":"code_execution","type":"tool-result"}]`
)

func r74genUsageNoTotal(in, out int64) *types.Usage {
	return &types.Usage{InputTokens: &in, OutputTokens: &out}
}

// TS asLanguageModelUsage computes totalTokens = input + output for every
// model response; a Go model that leaves TotalTokens nil gets it filled.
func TestR74Gen_UsageTotalTokensFilled(t *testing.T) {
	wantTotal := func(t *testing.T, name string, u types.Usage, want int64) {
		t.Helper()
		if u.TotalTokens == nil || *u.TotalTokens != want {
			t.Fatalf("%s TotalTokens = %v, want %d", name, u.TotalTokens, want)
		}
	}
	t.Run("GenerateText", func(t *testing.T) {
		step := &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop, Usage: *r74genUsageNoTotal(3, 10)}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: r72genSeqModel(t, step), Prompt: "p"})
		if err != nil {
			t.Fatal(err)
		}
		wantTotal(t, "step", res.Steps[0].Usage, 13)
		wantTotal(t, "total", res.TotalUsage, 13)
	})
	t.Run("only output tokens", func(t *testing.T) {
		out := int64(4)
		step := &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop, Usage: types.Usage{OutputTokens: &out}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: r72genSeqModel(t, step), Prompt: "p"})
		if err != nil {
			t.Fatal(err)
		}
		wantTotal(t, "step", res.Steps[0].Usage, 4)
	})
	t.Run("no counts stays nil", func(t *testing.T) {
		step := &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: r72genSeqModel(t, step), Prompt: "p"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Steps[0].Usage.TotalTokens != nil {
			t.Fatalf("TotalTokens = %v, want nil", *res.Steps[0].Usage.TotalTokens)
		}
	})
	t.Run("provider total kept", func(t *testing.T) {
		u := *r74genUsageNoTotal(3, 10)
		total := int64(20)
		u.TotalTokens = &total
		step := &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop, Usage: u}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: r72genSeqModel(t, step), Prompt: "p"})
		if err != nil {
			t.Fatal(err)
		}
		wantTotal(t, "step", res.Steps[0].Usage, 20)
	})
	t.Run("GenerateObject", func(t *testing.T) {
		model := &testutil.MockLanguageModel{
			StructuredSupport: true,
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{Text: `{ "content": "x" }`, FinishReason: types.FinishReasonStop, Usage: *r74genUsageNoTotal(3, 10)}, nil
			},
		}
		res, err := GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Prompt: "p", Schema: contentSchema()})
		if err != nil {
			t.Fatal(err)
		}
		wantTotal(t, "object", res.Usage, 13)
	})
	t.Run("StreamObject", func(t *testing.T) {
		chunks := append(textChunks(`{ "content": "x" }`), provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r74genUsageNoTotal(3, 10)})
		res, err := StreamObject(context.Background(), StreamObjectOptions{Model: streamObjectModel(chunks...), Prompt: "p", Schema: contentSchema()})
		if err != nil {
			t.Fatal(err)
		}
		wantTotal(t, "object", res.Usage, 13)
	})
}

// stream CR-1 (r74): TS step content tool-result `output` is the raw tool
// output, whatever toModelOutput returns; the model output is only in the
// response messages. Go keeps the raw value in Result next to Output.
func TestR74Gen_StepContentToolResultKeepsRawOutput(t *testing.T) {
	tool := types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"public": "p", "secret": "s"}, nil
		},
		ToModelOutput: func(_ context.Context, o types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
			return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "model sees p"}, nil
		},
	}
	plain := r72genValueTool("tool2")
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t,
			r72genCalls(
				types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}},
				types.ToolCall{ID: "call-2", ToolName: "tool2", Arguments: map[string]interface{}{"value": "v"}}),
			r72genText("done")),
		Prompt:   "go",
		Tools:    []types.Tool{tool, plain},
		StopWhen: []StopCondition{StepCountIs(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]types.ToolResultContent{}
	for _, p := range res.Steps[0].Content {
		if tr, ok := p.(types.ToolResultContent); ok {
			got[tr.ToolCallID] = tr
		}
	}
	if tr := got["call-1"]; !reflect.DeepEqual(tr.Result, map[string]interface{}{"public": "p", "secret": "s"}) || tr.Output == nil || tr.Output.Value != "model sees p" {
		t.Fatalf("call-1 content = %+v", tr)
	}
	if tr := got["call-2"]; tr.Result != "v-result" || tr.Output == nil || tr.Output.Type != types.ToolResultOutputText || tr.Output.Value != "v-result" {
		t.Fatalf("call-2 content = %+v", tr)
	}
	// The response messages (and so the next prompt) carry the model output only.
	for _, m := range res.ResponseMessages {
		for _, p := range m.Content {
			if tr, ok := p.(types.ToolResultContent); ok && tr.Result != nil {
				t.Fatalf("response message tool result keeps Result: %+v", tr)
			}
		}
	}
}

// generate-text.test.ts 9780 'should stop for client tool approval while a
// provider tool result is deferred'.
func TestR74Gen_ApprovalWhileProviderResultDeferred(t *testing.T) {
	executed := 0
	modelCalls := 0
	model := &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			modelCalls++
			return &types.GenerateResult{
				ToolCalls: []types.ToolCall{
					{ID: "program-call", ToolName: "program", RawArguments: `{"code":"getHours()"}`, ProviderExecuted: true},
					{ID: "get-hours-call", ToolName: "getHours", RawArguments: `{"member":"Ada"}`},
				},
				FinishReason: types.FinishReasonToolCalls, Usage: *usage(3, 10),
			}, nil
		},
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Get Ada hours with a program.",
		Tools: []types.Tool{
			{Name: "program", Type: "provider", ProviderID: "test.program", ProviderArgs: map[string]interface{}{}, ProviderExecuted: true, SupportsDeferredResults: true,
				Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"code": map[string]interface{}{"type": "string"}}, "required": []interface{}{"code"}}},
			{Name: "getHours", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"member": map[string]interface{}{"type": "string"}}, "required": []interface{}{"member"}},
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
	if modelCalls != 1 || executed != 0 || len(res.Steps) != 1 {
		t.Fatalf("modelCalls=%d executed=%d steps=%d, want 1/0/1", modelCalls, executed, len(res.Steps))
	}
	var requests []string
	for _, p := range res.Content {
		if r, ok := p.(types.ToolApprovalRequestContent); ok {
			requests = append(requests, r.ToolCall.ID+":"+r.ToolCall.ToolName)
		}
	}
	if !reflect.DeepEqual(requests, []string{"get-hours-call:getHours"}) {
		t.Fatalf("approval requests = %v", requests)
	}
}

// generate-text.test.ts 1216 'should include custom parts in content'.
func TestR74Gen_CustomPartsInContent(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Content: []types.ContentPart{
					types.TextContent{Text: "Hello"},
					types.CustomContent{Kind: "openai.compaction", ProviderMetadata: json.RawMessage(`{"openai":{"itemId":"cmp_123"}}`)},
					types.CustomContent{Kind: "openai.other"},
				},
				FinishReason: types.FinishReasonStop, Usage: *usage(3, 10),
			}, nil
		},
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	want := []types.ContentPart{
		types.TextContent{Text: "Hello"},
		types.CustomContent{Kind: "openai.compaction", ProviderMetadata: json.RawMessage(`{"openai":{"itemId":"cmp_123"}}`)},
		types.CustomContent{Kind: "openai.other"},
	}
	if !reflect.DeepEqual(res.Content, want) {
		t.Fatalf("content = %#v\nwant %#v", res.Content, want)
	}
}

// generate-text.test.ts 1578 'should download URL-backed %s parts once', the
// reasoning-file case (the file case is
// TestGeneratedFileDownload_GenerateEmitsDownloadedContent).
func TestR74Gen_ReasoningFileURLDownloadedOnce(t *testing.T) {
	var hits atomic.Int32
	url := fileServer(t, http.StatusOK, &hits)
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Content:      []types.ContentPart{types.ReasoningFileContent{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeURL, URL: url}}},
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, p := range res.Content {
		if f, ok := p.(types.ReasoningFileContent); ok {
			found = true
			if string(f.Data) != "Hello World" {
				t.Fatalf("reasoning file = %+v, want the downloaded bytes", f)
			}
		}
	}
	if !found {
		t.Fatalf("content = %#v, want a reasoning file", res.Content)
	}
	if hits.Load() != 1 {
		t.Fatalf("downloads = %d, want 1", hits.Load())
	}
}

type r74genLMCallTelemetry struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	starts int
	ends   []telemetry.LanguageModelCallEndEvent
}

func (r *r74genLMCallTelemetry) OnLanguageModelCallStart(ctx context.Context, _ telemetry.LanguageModelCallStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts++
	return ctx
}

func (r *r74genLMCallTelemetry) OnLanguageModelCallEnd(_ context.Context, e telemetry.LanguageModelCallEndEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, e)
}

// generate-text.test.ts 2141 'should report the completed model call before
// rejecting a tool choice violation'.
func TestR74Gen_ModelCallEndBeforeToolChoiceViolation(t *testing.T) {
	model := &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Content:          []types.ContentPart{types.TextContent{Text: "No tool call."}},
				FinishReason:     types.FinishReasonStop,
				Usage:            *usage(3, 10),
				ProviderMetadata: map[string]interface{}{"testProvider": map[string]interface{}{"requestId": "request-1"}},
				ResponseMetadata: &types.ResponseMetadata{ID: "response-1", Timestamp: time.Unix(0, 0), ModelID: "response-model-id"},
			}, nil
		},
	}
	tel := &r74genLMCallTelemetry{}
	var starts int
	var ends []LanguageModelCallEndEvent
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:      model,
		Tools:      []types.Tool{{Name: "tool1", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}}}},
		ToolChoice: types.RequiredToolChoice(),
		Prompt:     "test-input",
		OnLanguageModelCallStart: func(context.Context, LanguageModelCallStartEvent) {
			starts++
		},
		OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) {
			ends = append(ends, e)
		},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{tel}},
	})
	var violation *ToolChoiceViolationError
	if !errors.As(err, &violation) {
		t.Fatalf("err = %v, want ToolChoiceViolationError", err)
	}
	if starts != 1 || len(ends) != 1 || tel.starts != 1 || len(tel.ends) != 1 {
		t.Fatalf("starts=%d ends=%d telemetry starts=%d ends=%d, want 1 each", starts, len(ends), tel.starts, len(tel.ends))
	}
	e := ends[0]
	if e.CallID == "" || e.Provider != "mock-provider" || e.ModelID != "response-model-id" || e.FinishReason != types.FinishReasonStop ||
		e.ResponseID != "response-1" || !reflect.DeepEqual(e.ProviderMetadata, map[string]interface{}{"testProvider": map[string]interface{}{"requestId": "request-1"}}) ||
		!reflect.DeepEqual(e.Content, []types.ContentPart{types.TextContent{Text: "No tool call."}}) ||
		e.Usage.InputTokens == nil || *e.Usage.InputTokens != 3 || *e.Usage.OutputTokens != 10 || *e.Usage.TotalTokens != 13 {
		t.Fatalf("model call end = %+v", e)
	}
	te := tel.ends[0]
	if te.CallID != e.CallID || te.ModelProvider != "mock-provider" || te.ModelID != "response-model-id" || te.FinishReason != "stop" ||
		te.ResponseID != "response-1" || te.Usage.TotalTokens == nil || *te.Usage.TotalTokens != 13 {
		t.Fatalf("telemetry model call end = %+v", te)
	}
}
