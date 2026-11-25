package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type r59AbortTelemetry struct {
	telemetry.NoopTelemetryIntegration
	mu                         sync.Mutex
	stepErrors, errors, aborts int
}

func (r *r59AbortTelemetry) OnStepError(context.Context, telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	r.stepErrors++
	r.mu.Unlock()
}

func (r *r59AbortTelemetry) OnError(context.Context, telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	r.errors++
	r.mu.Unlock()
}

func (r *r59AbortTelemetry) OnAbort(context.Context, telemetry.TelemetryAbortEvent) {
	r.mu.Lock()
	r.aborts++
	r.mu.Unlock()
}

func r59ToolCallStream() provider.TextStream {
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
	})
}

// TS streamText: notifyStepError returns early once the abort signal fired,
// so the telemetry onStepError is never called for an aborted step. A first
// model call that fails to open after a cancel reaches the pipeline catch
// (telemetry onError, then the outer stream's abort(): onAbort). A later step
// that fails after a cancel is caught by the step catch, which only enqueues
// an `error` part; the outer stream turns it into abort(): telemetry onAbort
// only.
func TestR59StreamTextAbortedStepTelemetry(t *testing.T) {
	tool := []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "r", nil
	}}}
	cases := []struct {
		name       string
		build      func(cancel context.CancelFunc) StreamTextOptions
		wantErrors int
	}{
		{
			name: "first model call fails to open",
			build: func(cancel context.CancelFunc) StreamTextOptions {
				return StreamTextOptions{Prompt: "a", Model: &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
					cancel()
					return nil, ctx.Err()
				}}}
			},
			wantErrors: 1,
		},
		{
			name: "next model call fails to open",
			build: func(cancel context.CancelFunc) StreamTextOptions {
				calls := 0
				return StreamTextOptions{Prompt: "a", Tools: tool, StopWhen: []StopCondition{IsStepCount(3)}, Model: &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
					calls++
					if calls == 1 {
						return r59ToolCallStream(), nil
					}
					cancel()
					return nil, ctx.Err()
				}}}
			},
		},
		{
			name: "next step preparation fails",
			build: func(cancel context.CancelFunc) StreamTextOptions {
				return StreamTextOptions{Prompt: "a", Tools: tool, StopWhen: []StopCondition{IsStepCount(3)},
					Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
						return r59ToolCallStream(), nil
					}},
					PrepareStep: func(_ context.Context, o PrepareStepOptions) PrepareStepOptions {
						if o.StepNumber == 1 {
							cancel()
							zero := 0
							return PrepareStepOptions{MaxOutputTokens: &zero}
						}
						return PrepareStepOptions{}
					}}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := tc.build(cancel)
			rec := &r59AbortTelemetry{}
			// The telemetry onAbort alone ends every span the step opened.
			spans := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
			defer func() { _ = tp.Shutdown(context.Background()) }()
			opts.Telemetry = &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{
				rec,
				telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("genai")}),
				telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("legacy")}),
			}}
			res, err := StreamText(ctx, opts)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = r58FullStreamParts(t, res)
			if started, ended := len(spans.Started()), len(spans.Ended()); started == 0 || started != ended {
				t.Fatalf("spans started = %d, ended = %d; want every started span ended", started, ended)
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if rec.stepErrors != 0 || rec.errors != tc.wantErrors || rec.aborts != 1 {
				t.Fatalf("telemetry OnStepError = %d, OnError = %d, OnAbort = %d; want 0, %d, 1", rec.stepErrors, rec.errors, rec.aborts, tc.wantErrors)
			}
		})
	}
}
