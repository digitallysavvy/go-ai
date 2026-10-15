package ai

import (
	"context"
	"errors"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestGenerateText_PrepareStepActiveTools(t *testing.T) {
	t.Parallel()

	var got []string
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			for _, tool := range opts.Tools {
				got = append(got, tool.Name)
			}
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}
	tools := []types.Tool{
		{Name: "a", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "b", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "c", Parameters: map[string]interface{}{"type": "object"}},
	}
	var startActive []string
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:       model,
		Prompt:      "hi",
		Tools:       tools,
		ActiveTools: []string{"a", "b"},
		PrepareStep: func(_ context.Context, _ PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{ActiveTools: []string{"c"}}
		},
		OnStepStart: func(_ context.Context, e OnStepStartEvent) { startActive = e.ActiveTools },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "c" {
		t.Fatalf("model tools = %v, want [c]", got)
	}
	if len(startActive) != 1 || startActive[0] != "c" {
		t.Fatalf("OnStepStart ActiveTools = %v, want [c]", startActive)
	}
}

func TestStreamText_PrepareStepActiveTools(t *testing.T) {
	t.Parallel()

	var got []string
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			for _, tool := range opts.Tools {
				got = append(got, tool.Name)
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	tools := []types.Tool{
		{Name: "a", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "b", Parameters: map[string]interface{}{"type": "object"}},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
		Tools:  tools,
		PrepareStep: func(_ context.Context, _ PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{ActiveTools: []string{"b"}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("model tools = %v, want [b]", got)
	}
}

func TestGenerateText_OutputParseErrorAfterEndCallbacks(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "not json", FinishReason: types.FinishReasonStop}, nil
		},
	}
	var stepEnds, ends int
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:     model,
		Prompt:    "hi",
		Output:    JSONOutput(JSONOutputOptions{}),
		OnStepEnd: func(context.Context, types.StepResult, interface{}) { stepEnds++ },
		OnEnd:     func(context.Context, *GenerateTextResult, interface{}) { ends++ },
	})
	if err == nil {
		t.Fatal("expected output parse error")
	}
	if stepEnds != 1 || ends != 1 {
		t.Fatalf("step-end=%d end=%d, want 1/1: TS parses output after onEnd", stepEnds, ends)
	}
}

func TestGenerateText_StepTimeoutCoversToolExecution(t *testing.T) {
	t.Parallel()

	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "slow", Arguments: map[string]interface{}{}}},
			}, nil
		},
	}
	var sawCancel bool
	tool := types.Tool{
		Name:       "slow",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			select {
			case <-ctx.Done():
				sawCancel = true
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return "late", nil
			}
		},
	}
	perStep := 50 * time.Millisecond
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "hi",
		Tools:    []types.Tool{tool},
		Timeout:  &TimeoutConfig{PerStep: &perStep},
		StopWhen: []StopCondition{IsStepCount(3)},
	})
	if !sawCancel {
		t.Fatal("step timeout did not cancel the running tool")
	}
	var te *TimeoutError
	if !errors.As(err, &te) || te.Reason != TimeoutReasonStep {
		t.Fatalf("err = %v, want step TimeoutError", err)
	}
	if calls != 1 {
		t.Fatalf("model calls = %d, want 1 (next step must not start)", calls)
	}
}

// TS notify() swallows callback errors, so a throwing onStepEnd/onEnd must not
// break the call. The Go equivalent is a panic in the callback.
func TestGenerateText_PanickingLegacyCallbacksDoNotBreakCall(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:     model,
		Prompt:    "hi",
		OnStepEnd: func(context.Context, types.StepResult, interface{}) { panic("step") },
		OnEnd:     func(context.Context, *GenerateTextResult, interface{}) { panic("end") },
	})
	if err != nil || res.Text != "ok" {
		t.Fatalf("res=%v err=%v", res, err)
	}
}

func TestStreamText_PanickingLegacyCallbacksDoNotBreakCall(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:     model,
		Prompt:    "hi",
		OnStepEnd: func(context.Context, types.StepResult, interface{}) { panic("step") },
		OnEnd:     func(*StreamTextResult) { panic("end") },
	})
	if err != nil {
		t.Fatal(err)
	}
	text, err := res.ReadAll()
	if err != nil || text != "ok" {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestEmbedMany_PanickingCallbacksDoNotBreakCall(t *testing.T) {
	t.Parallel()

	res, err := EmbedMany(context.Background(), EmbedManyOptions{
		Model:   &testutil.MockEmbeddingModel{},
		Inputs:  []string{"a", "b"},
		OnStart: func(EmbedOnStartEvent) { panic("start") },
		OnEnd:   func(EmbedOnFinishEvent) { panic("end") },
	})
	if err != nil || len(res.Embeddings) != 2 {
		t.Fatalf("res=%v err=%v", res, err)
	}
}

func TestExperimentalGetBatchResults_ValidatesToolCallsAgainstTools(t *testing.T) {
	t.Parallel()

	calls := []types.ToolCall{
		{ID: "ok", ToolName: "weather", Arguments: map[string]interface{}{"city": "Berlin"}},
		{ID: "bad", ToolName: "missing", Arguments: map[string]interface{}{}},
	}
	stream := &mockBatchItemStream{items: []provider.BatchV4ItemResult{{
		Type: provider.BatchRequestTypeText, ID: "req-1", Status: provider.BatchItemSucceeded,
		TextResult: &types.GenerateResult{ToolCalls: calls, FinishReason: types.FinishReasonToolCalls},
	}}}
	mock := &mockBatchV4{
		providerName: "mock.batch",
		resultsFn: func(context.Context, provider.BatchV4OperationOptions) (provider.BatchV4ItemResultStream, error) {
			return stream, nil
		},
	}
	rs, err := ExperimentalGetBatchResults(context.Background(), GetBatchResultsOptions{
		Provider: mock,
		Batch:    BatchReference{Version: 2, ID: "job_1", Provider: "mock.batch"},
		Tools: []types.Tool{{
			Name: "weather",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
				"required":   []interface{}{"city"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := rs.Next()
	if err != nil {
		t.Fatal(err)
	}
	got := item.Text.ToolCalls
	if len(got) != 2 || got[0].Invalid || !got[1].Invalid || got[1].Error == nil {
		t.Fatalf("tool calls = %+v, want first valid and second invalid", got)
	}
}

// TS detects prompt/frame image media types with the full image signature
// table (bmp, tiff, avif, heic, ...), not just png/gif/jpeg/webp.
func TestNormalizeVideoInputs_DetectsFullImageSignatureTable(t *testing.T) {
	t.Parallel()

	bmp := []byte{0x42, 0x4d, 1, 2, 3, 4, 0, 0, 0, 0, 9, 9}
	got, err := normalizeVideoInputs(VideoPrompt{Text: "x", Image: &VideoPromptImage{Data: bmp}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Image == nil || got.Image.MediaType != "image/bmp" {
		t.Fatalf("image = %+v, want image/bmp", got.Image)
	}

	tiff := []byte{0x49, 0x49, 0x2a, 0x00, 1, 2}
	ref, err := normalizeVideoInputs(VideoPrompt{Text: "x"}, nil, []VideoReferenceInput{{Data: VideoPromptImage{Data: tiff}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ref.InputReferences) != 1 || ref.InputReferences[0].MediaType != "image/tiff" {
		t.Fatalf("reference = %+v, want image/tiff", ref.InputReferences)
	}
}

// TS stream-text forwards an empty text-delta when it carries provider
// metadata (e.g. a Gemini thought signature) and drops it otherwise.
func TestStreamText_ForwardsEmptyTextDeltaWithProviderMetadata(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "t1"},
				{Type: provider.ChunkTypeText, ID: "t1", Text: "hi"},
				{Type: provider.ChunkTypeText, ID: "t1", Text: ""},
				{Type: provider.ChunkTypeText, ID: "t1", Text: "", ProviderMetadata: []byte(`{"google":{"thoughtSignature":"sig"}}`)},
				{Type: provider.ChunkTypeTextEnd, ID: "t1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for c := range res.Chunks() {
		if c.Type == provider.ChunkTypeText {
			texts = append(texts, fmt.Sprintf("%q/%d", c.Text, len(c.ProviderMetadata)))
		}
	}
	if len(texts) != 2 || texts[0] != `"hi"/0` || !strings.HasPrefix(texts[1], `""/`) || texts[1] == `""/0` {
		t.Fatalf("forwarded text chunks = %v, want hi and the empty one carrying metadata", texts)
	}
}

type stepEndUsageRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	inputs []int64
}

func (r *stepEndUsageRecorder) OnStepEnd(_ context.Context, e telemetry.TelemetryStepEndEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.Usage.InputTokens != nil {
		r.inputs = append(r.inputs, *e.Usage.InputTokens)
	}
}

// The step-end telemetry event carries that step's usage (generateText does);
// streamText used to hand over the running total across steps.
func TestStreamText_StepEndTelemetryUsageIsPerStep(t *testing.T) {
	t.Parallel()

	in1, in2 := int64(10), int64(7)
	var calls int32
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			if atomic.AddInt32(&calls, 1) == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{InputTokens: &in1}},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{InputTokens: &in2}},
			}), nil
		},
	}
	rec := &stepEndUsageRecorder{}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "hi",
		StopWhen: []StopCondition{IsStepCount(3)},
		Tools: []types.Tool{{
			Name:       "t",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}},
		Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.inputs) != 2 || rec.inputs[0] != 10 || rec.inputs[1] != 7 {
		t.Fatalf("per-step input tokens = %v, want [10 7] (not the running total [10 17])", rec.inputs)
	}
}
