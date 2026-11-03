package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providerErrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r83genNoopTool(name string) types.Tool {
	return types.Tool{
		Name:       name,
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, nil
		},
	}
}

func r83genLastUserText(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != types.RoleUser {
			continue
		}
		var b strings.Builder
		for _, p := range msgs[i].Content {
			if tc, ok := p.(types.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return b.String()
	}
	return ""
}

// TS generate-text.test.ts "re-announces a caller message when the catalog
// reverts": the caller message follows the active tools of each step, and a
// catalog that changes back is announced again.
func TestR83genToolCallersReannounceOnCatalogRevert(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var lastUser []string
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			lastUser = append(lastUser, r83genLastUserText(opts.Prompt.Messages))
			n := len(lastUser)
			mu.Unlock()
			if n < 3 {
				return toolCallResponse(fmt.Sprintf("call-%d", n), "code_mode", map[string]interface{}{}), nil
			}
			return &types.GenerateResult{Text: "Done.", FinishReason: types.FinishReasonStop}, nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Use the available tool.",
		Tools: []types.Tool{
			localCallerTool("code_mode", func(tools []types.Tool) *string {
				names := make([]string, len(tools))
				for i, tl := range tools {
					names[i] = tl.Name
				}
				s := "Available caller tools: " + strings.Join(names, ", ") + "."
				return &s
			}),
			r83genNoopTool("toolA"),
			r83genNoopTool("toolB"),
		},
		ExperimentalToolCallers: ExperimentalToolCallers{
			"toolA": {"code_mode"},
			"toolB": {"code_mode"},
		},
		StopWhen: []StopCondition{IsStepCount(3)},
		PrepareStep: func(ctx context.Context, step PrepareStepOptions) PrepareStepOptions {
			if step.StepNumber == 1 {
				step.ActiveTools = []string{"code_mode", "toolB"}
			} else {
				step.ActiveTools = []string{"code_mode", "toolA"}
			}
			return step
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	want := []string{
		"Available caller tools: toolA.",
		"Available caller tools: toolB.",
		"Available caller tools: toolA.",
	}
	if fmt.Sprint(lastUser) != fmt.Sprint(want) {
		t.Fatalf("last user messages = %q, want %q", lastUser, want)
	}
}

func r83genTool1(execute bool) types.Tool {
	tl := types.Tool{
		Name: "tool1",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"value"},
		},
	}
	if execute {
		tl.Execute = func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return fmt.Sprint(input["value"]) + "-result", nil
		}
	}
	return tl
}

// r83genSeqModel returns a tool1 call with each value in turn, then text.
func r83genSeqModel(values ...string) *testutil.MockLanguageModel {
	var mu sync.Mutex
	n := 0
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			i := n
			n++
			mu.Unlock()
			if i < len(values) {
				return toolCallResponse(fmt.Sprintf("call-%d", i+1), "tool1", map[string]interface{}{"value": values[i]}), nil
			}
			return &types.GenerateResult{Text: "Final answer.", FinishReason: types.FinishReasonStop}, nil
		},
	}
}

// TS "options.onStepStart > should provide previous step results in steps
// array for subsequent steps".
func TestR83genOnStepStartPreviousSteps(t *testing.T) {
	t.Parallel()
	var events []OnStepStartEvent
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:       r83genSeqModel("step0", "step1"),
		Prompt:      "test-input",
		Tools:       []types.Tool{r83genTool1(true)},
		StopWhen:    []StopCondition{IsStepCount(4)},
		OnStepStart: func(_ context.Context, e OnStepStartEvent) { events = append(events, e) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("%d step start events, want 3", len(events))
	}
	if len(events[0].Steps) != 0 || len(events[1].Steps) != 1 || len(events[2].Steps) != 2 {
		t.Fatalf("steps lengths %d %d %d", len(events[0].Steps), len(events[1].Steps), len(events[2].Steps))
	}
	s := events[1].Steps[0]
	if s.FinishReason != types.FinishReasonToolCalls || len(s.ToolCalls) != 1 || s.ToolCalls[0].ToolName != "tool1" ||
		len(s.ToolResults) != 1 || s.ToolResults[0].Result != "step0-result" {
		t.Fatalf("step 0 as seen by step 1: %+v", s)
	}
	if events[2].Steps[1].FinishReason != types.FinishReasonToolCalls || events[2].Steps[1].ToolResults[0].Result != "step1-result" {
		t.Fatalf("step 1 as seen by step 2: %+v", events[2].Steps[1])
	}
}

// TS "options.onStepFinish stepNumber > should have matching stepNumber
// between onStepStart and onStepFinish".
func TestR83genStepNumbersMatchStartAndFinish(t *testing.T) {
	t.Parallel()
	var starts, finishes []int
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:        r83genSeqModel("test"),
		Prompt:       "test-input",
		Tools:        []types.Tool{r83genTool1(true)},
		StopWhen:     []StopCondition{IsStepCount(3)},
		OnStepStart:  func(_ context.Context, e OnStepStartEvent) { starts = append(starts, e.StepNumber) },
		OnStepFinish: func(_ context.Context, s types.StepResult, _ interface{}) { finishes = append(finishes, s.StepNumber) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(starts) != fmt.Sprint(finishes) || len(starts) != 2 {
		t.Fatalf("start %v finish %v", starts, finishes)
	}
}

// TS "onToolExecutionStart/End > should not fire for tools without execute".
func TestR83genToolExecutionEventsSkipToolsWithoutExecute(t *testing.T) {
	t.Parallel()
	var events []string
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  r83genSeqModel("test"),
		Prompt: "test-input",
		Tools:  []types.Tool{r83genTool1(false)},
		OnToolExecutionStart: func(context.Context, OnToolCallStartEvent) {
			events = append(events, "start")
		},
		OnToolExecutionEnd: func(context.Context, OnToolCallFinishEvent) {
			events = append(events, "end")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("events %v, want none", events)
	}
}

// TS "onToolExecutionEnd > should use experimental_onToolCallFinish as a
// fallback" and "should prefer onToolExecutionEnd over
// experimental_onToolCallFinish" (Go OnToolCallFinish is the deprecated
// alias).
func TestR83genToolExecutionEndFallback(t *testing.T) {
	t.Parallel()
	run := func(withEnd bool) []string {
		var order []string
		opts := GenerateTextOptions{
			Model:            r83genSeqModel("test"),
			Prompt:           "test-input",
			Tools:            []types.Tool{r83genTool1(true)},
			StopWhen:         []StopCondition{IsStepCount(1)},
			OnToolCallFinish: func(context.Context, OnToolCallFinishEvent) { order = append(order, "experimental_onToolCallFinish") },
		}
		if withEnd {
			opts.OnToolExecutionEnd = func(context.Context, OnToolCallFinishEvent) { order = append(order, "onToolExecutionEnd") }
		}
		if _, err := GenerateText(context.Background(), opts); err != nil {
			t.Fatal(err)
		}
		return order
	}
	if got := run(false); fmt.Sprint(got) != "[experimental_onToolCallFinish]" {
		t.Fatalf("fallback: %v", got)
	}
	if got := run(true); fmt.Sprint(got) != "[onToolExecutionEnd]" {
		t.Fatalf("prefer: %v", got)
	}
}

// TS "onToolExecutionEnd > should pass context on error": the validated tool
// context and the error reach the end event of a failed execution.
func TestR83genToolExecutionEndContextOnError(t *testing.T) {
	t.Parallel()
	toolErr := fmt.Errorf("Tool execution failed")
	tl := r83genTool1(false)
	tl.Execute = func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
		return nil, toolErr
	}
	var events []OnToolCallFinishEvent
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:              r83genSeqModel("test"),
		Prompt:             "prompt",
		Tools:              []types.Tool{tl},
		StopWhen:           []StopCondition{IsStepCount(1)},
		ToolsContext:       map[string]interface{}{"tool1": map[string]interface{}{"context": "test"}},
		OnToolExecutionEnd: func(_ context.Context, e OnToolCallFinishEvent) { events = append(events, e) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("%d end events", len(events))
	}
	e := events[0]
	if fmt.Sprint(e.ToolContext) != "map[context:test]" || e.ToolOutput.Error == nil || e.ToolCall.ID != "call-1" ||
		fmt.Sprint(e.ToolCall.Arguments) != "map[value:test]" {
		t.Fatalf("event %+v", e)
	}
}

// TS "provider-selected structured output > should reject invalid selected
// text %j without falling back to valid content": an empty or truncated
// structuredOutputText is parsed (and fails) even though the full text is
// valid JSON.
func TestR83genInvalidStructuredOutputTextRejected(t *testing.T) {
	t.Parallel()
	for _, structured := range []string{"", `{"value":`} {
		structured := structured
		model := &testutil.MockLanguageModel{
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{Text: `{"value":"draft"}`, StructuredOutputText: &structured, FinishReason: types.FinishReasonStop}, nil
			},
		}
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model:  model,
			Prompt: "prompt",
			Output: ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: r31ObjectSchema}),
		})
		var noObj *NoObjectGeneratedError
		if !errors.As(err, &noObj) {
			t.Fatalf("%q: err = %v, want NoObjectGeneratedError", structured, err)
		}
		var parseErr *providerErrors.JSONParseError
		if noObj.Text != structured || !errors.As(noObj.Cause, &parseErr) {
			t.Fatalf("%q: text %q cause %T %v", structured, noObj.Text, noObj.Cause, noObj.Cause)
		}
	}
}

// TS "options.stopWhen > 2 stop conditions": every stop condition is called
// with the steps (Promise.all), not only up to the first that is met; the
// loop stops after one step.
func TestR83genEveryStopConditionCalled(t *testing.T) {
	t.Parallel()
	type call struct{ number, steps int }
	var calls []call
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  r83genSeqModel("value"),
		Prompt: "test-input",
		Tools:  []types.Tool{r83genTool1(true)},
		StopWhen: []StopCondition{
			func(s StopConditionState) string { calls = append(calls, call{0, len(s.Steps)}); return "" },
			func(s StopConditionState) string { calls = append(calls, call{1, len(s.Steps)}); return "stop" },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 || fmt.Sprint(calls) != "[{0 1} {1 1}]" {
		t.Fatalf("steps %d calls %v", len(res.Steps), calls)
	}
}
