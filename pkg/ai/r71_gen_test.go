package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r71genOrderLog struct {
	mu  sync.Mutex
	got []string
}

func (l *r71genOrderLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = append(l.got, s)
}

func (l *r71genOrderLog) check(t *testing.T, want []string) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if strings.Join(l.got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", l.got, want)
	}
}

// r71genOrderTelemetry records telemetry events in the shared order log.
type r71genOrderTelemetry struct {
	telemetry.NoopTelemetryIntegration
	log *r71genOrderLog
}

func (r *r71genOrderTelemetry) OnToolExecutionEnd(context.Context, telemetry.TelemetryToolCallFinishEvent) {
	r.log.add("telemetry.onToolExecutionEnd")
}

func (r *r71genOrderTelemetry) OnStepEnd(context.Context, telemetry.TelemetryStepEndEvent) {
	r.log.add("telemetry.onStepEnd")
}

func (r *r71genOrderTelemetry) OnFinish(context.Context, telemetry.TelemetryFinishEvent) {
	r.log.add("telemetry.onEnd")
}

func (r *r71genOrderTelemetry) OnDecideStart(ctx context.Context, _ telemetry.DecideStartEvent) context.Context {
	r.log.add("telemetry.onStart")
	return ctx
}

func (r *r71genOrderTelemetry) OnDecideEnd(context.Context, telemetry.DecideEndEvent) {
	r.log.add("telemetry.onEnd")
}

func r71genTelemetry(log *r71genOrderLog) *telemetry.Options {
	return &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{&r71genOrderTelemetry{log: log}}}
}

var r71genObjectSchema = schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

func r71genEchoTool(toModel func(context.Context, types.ToModelOutputOptions) (*types.ToolResultOutput, error)) types.Tool {
	return types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "raw", nil
		},
		ToModelOutput: toModel,
	}
}

// TS executeToolCall notifies [onToolExecutionEnd,
// telemetry.onToolExecutionEnd] (generate-text.ts and stream-text.ts): the
// user callback starts first.
func TestR71Gen_ToolExecutionEndRunsBeforeTelemetry(t *testing.T) {
	call := types.ToolCall{ID: "call-1", ToolName: "t", Arguments: map[string]interface{}{}}
	want := []string{"onToolExecutionEnd", "telemetry.onToolExecutionEnd", "telemetry.onStepEnd", "telemetry.onEnd"}

	log := &r71genOrderLog{}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{ToolCalls: []types.ToolCall{call}, FinishReason: types.FinishReasonToolCalls}, nil
		},
	}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "p", Tools: []types.Tool{r71genEchoTool(nil)},
		Telemetry:          r71genTelemetry(log),
		OnToolExecutionEnd: func(context.Context, OnToolCallFinishEvent) { log.add("onToolExecutionEnd") },
	}); err != nil {
		t.Fatal(err)
	}
	log.check(t, want)

	log = &r71genOrderLog{}
	smodel := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &call},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: smodel, Prompt: "p", Tools: []types.Tool{r71genEchoTool(nil)},
		Telemetry:          r71genTelemetry(log),
		OnToolExecutionEnd: func(context.Context, OnToolCallFinishEvent) { log.add("onToolExecutionEnd") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	log.check(t, want)
}

// TS converts a tool's output for the model (createToolModelOutput) only
// after executeToolCall has notified onToolExecutionEnd, so a failing
// ToModelOutput still ends the execution (user callback, then telemetry)
// before GenerateText fails with the conversion error.
func TestR71Gen_ToModelOutputErrorStillEndsTheExecution(t *testing.T) {
	log := &r71genOrderLog{}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "t", Arguments: map[string]interface{}{}}}, FinishReason: types.FinishReasonToolCalls}, nil
		},
	}
	var ended OnToolCallFinishEvent
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "p",
		Tools: []types.Tool{r71genEchoTool(func(context.Context, types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
			return nil, errors.New("convert failed")
		})},
		Telemetry: r71genTelemetry(log),
		OnToolExecutionEnd: func(_ context.Context, e OnToolCallFinishEvent) {
			ended = e
			log.add("onToolExecutionEnd")
		},
	})
	if err == nil || err.Error() != "convert failed" {
		t.Fatalf("err = %v, want the conversion error", err)
	}
	log.check(t, []string{"onToolExecutionEnd", "telemetry.onToolExecutionEnd"})
	if ended.ToolCallID != "call-1" || ended.ToolOutput.Result != "raw" || ended.ToolOutput.Error != nil || ended.ToolOutput.ModelOutput != nil {
		t.Fatalf("tool end event = %+v", ended)
	}
}

// TS generate-object.ts notify([onStepEnd, telemetry.onObjectStepEnd]) and
// notify([onFinish, telemetry.onEnd]): the user callbacks first.
func TestR71Gen_GenerateObjectCallbacksRunBeforeTelemetry(t *testing.T) {
	log := &r71genOrderLog{}
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"a":1}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	if _, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model: model, Prompt: "p", Schema: r71genObjectSchema,
		Telemetry: r71genTelemetry(log),
		OnStepEnd: func(context.Context, ObjectOnStepFinishEvent) { log.add("onStepEnd") },
		OnEnd:     func(context.Context, ObjectOnFinishEvent) { log.add("onEnd") },
	}); err != nil {
		t.Fatal(err)
	}
	log.check(t, []string{"onStepEnd", "telemetry.onStepEnd", "onEnd", "telemetry.onEnd"})
}

// TS stream-object.ts parses (and repairs) the final JSON in the finish part
// handler, then its flush handler notifies [onStepFinish,
// telemetry.onObjectStepEnd] and [onFinish, telemetry.onEnd]: RepairText runs
// before OnStepEnd and the user callbacks start before the telemetry. An
// error part takes the same flush path.
func TestR71Gen_StreamObjectRepairsBeforeStepEndAndCallbacksRunFirst(t *testing.T) {
	stream := func(chunks ...provider.StreamChunk) *testutil.MockLanguageModel {
		return &testutil.MockLanguageModel{
			StructuredSupport: true,
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream(chunks), nil
			},
		}
	}
	finish := provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}

	log := &r71genOrderLog{}
	var stepText string
	res, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: stream(provider.StreamChunk{Type: provider.ChunkTypeText, Text: `{"a":1`}, finish), Prompt: "p", Schema: r71genObjectSchema,
		Telemetry: r71genTelemetry(log),
		RepairText: func(_ context.Context, text string, _ error) (*string, error) {
			log.add("repairText")
			fixed := text + "}"
			return &fixed, nil
		},
		OnStepEnd: func(_ context.Context, e ObjectOnStepFinishEvent) {
			stepText = e.ObjectText
			log.add("onStepEnd")
		},
		OnEnd: func(context.Context, ObjectOnFinishEvent) { log.add("onEnd") },
	})
	if err != nil {
		t.Fatal(err)
	}
	log.check(t, []string{"repairText", "onStepEnd", "telemetry.onStepEnd", "onEnd", "telemetry.onEnd"})
	if obj, _ := res.Object.(map[string]interface{}); obj["a"] != float64(1) {
		t.Fatalf("object = %#v", res.Object)
	}
	// The step event keeps the model's text (TS objectText: accumulatedText).
	if stepText != `{"a":1` {
		t.Fatalf("step objectText = %q", stepText)
	}

	log = &r71genOrderLog{}
	_, err = StreamObject(context.Background(), StreamObjectOptions{
		Model: stream(provider.StreamChunk{Type: provider.ChunkTypeError, Err: errors.New("bad")}, finish), Prompt: "p", Schema: r71genObjectSchema,
		Telemetry: r71genTelemetry(log),
		OnStepEnd: func(context.Context, ObjectOnStepFinishEvent) { log.add("onStepEnd") },
		OnEnd:     func(context.Context, ObjectOnFinishEvent) { log.add("onEnd") },
	})
	if err == nil {
		t.Fatal("want the error part's error")
	}
	log.check(t, []string{"onStepEnd", "telemetry.onStepEnd", "onEnd", "telemetry.onEnd"})
}

// TS decide.ts notify([onStart, telemetry.onStart]) and notify([onEnd,
// telemetry.onEnd]): the user callbacks first. Decide's OnStart takes no
// context, so it does not depend on the telemetry span.
func TestR71Gen_DecideCallbacksRunBeforeTelemetry(t *testing.T) {
	log := &r71genOrderLog{}
	model := &mockDecisionModel{providerName: "p", modelID: "m", doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
		return &provider.DecisionResult{Answers: decisionAnswersFixture()}, nil
	}}
	if _, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: model, State: "message", Questions: decisionQuestionsFixture(),
		Telemetry: r71genTelemetry(log),
		OnStart:   func(DecideOnStartEvent) { log.add("onStart") },
		OnEnd:     func(DecideOnEndEvent) { log.add("onEnd") },
	}); err != nil {
		t.Fatal(err)
	}
	log.check(t, []string{"onStart", "telemetry.onStart", "onEnd", "telemetry.onEnd"})
}
