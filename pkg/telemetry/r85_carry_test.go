package telemetry

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS 4d6d830c14 gen-ai-format-messages.test.ts: the WorkflowAgent root
// operation maps to invoke_agent.
func TestR85carryWorkflowAgentOperationName(t *testing.T) {
	if got := mapOperationName("ai.workflowAgent.stream"); got != "invoke_agent" {
		t.Fatalf("mapOperationName(ai.workflowAgent.stream) = %q, want invoke_agent", got)
	}
}

// r85carryMeterProvider records histogram points (a stand-in for the OTel
// metrics SDK, which is not a module dependency).
type r85carryMeterProvider struct {
	noop.MeterProvider
	mu     sync.Mutex
	meters []string
	hists  []metric.Float64HistogramConfig
	names  []string
	points []r85carryPoint
}

type r85carryPoint struct {
	value float64
	attrs map[string]string
}

func (p *r85carryMeterProvider) Meter(name string, _ ...metric.MeterOption) metric.Meter {
	p.mu.Lock()
	p.meters = append(p.meters, name)
	p.mu.Unlock()
	return &r85carryMeter{p: p}
}

type r85carryMeter struct {
	noop.Meter
	p *r85carryMeterProvider
}

func (m *r85carryMeter) Float64Histogram(name string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	m.p.mu.Lock()
	m.p.names = append(m.p.names, name)
	m.p.hists = append(m.p.hists, metric.NewFloat64HistogramConfig(opts...))
	m.p.mu.Unlock()
	return &r85carryHistogram{p: m.p}, nil
}

type r85carryHistogram struct {
	noop.Float64Histogram
	p *r85carryMeterProvider
}

func (h *r85carryHistogram) Record(_ context.Context, v float64, opts ...metric.RecordOption) {
	cfg := metric.NewRecordConfig(opts)
	set := cfg.Attributes()
	attrs := map[string]string{}
	for _, kv := range set.ToSlice() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	h.p.mu.Lock()
	h.p.points = append(h.p.points, r85carryPoint{value: v, attrs: attrs})
	h.p.mu.Unlock()
}

func (p *r85carryMeterProvider) recorded() []r85carryPoint {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]r85carryPoint(nil), p.points...)
}

type r85carryCall struct {
	t     *testing.T
	i     OpenTelemetry
	ctx   context.Context
	id    string
	set   *Settings
	model string
}

func r85carryStart(t *testing.T, mp metric.MeterProvider, op string) *r85carryCall {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	c := &r85carryCall{t: t, i: NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai-test"), MeterProvider: mp}), id: t.Name(), set: &Settings{IsEnabled: Bool(true)}, model: "gpt-4"}
	c.ctx = c.i.OnStart(context.Background(), TelemetryStartEvent{OperationType: op, Settings: c.set, CallID: c.id, ModelID: c.model, ModelProvider: "openai.chat"})
	c.ctx = c.i.OnStepStart(c.ctx, TelemetryStepStartEvent{Settings: c.set, OperationType: op, CallID: c.id})
	return c
}

func (c *r85carryCall) callStart() {
	c.ctx = c.i.OnLanguageModelCallStart(c.ctx, LanguageModelCallStartEvent{Settings: c.set, CallID: c.id, ModelID: c.model, ModelProvider: "openai.chat"})
}

func (c *r85carryCall) callEnd(ms int64, finish, responseModel string) {
	c.i.OnLanguageModelCallEnd(c.ctx, LanguageModelCallEndEvent{Settings: c.set, CallID: c.id, ModelID: responseModel, ModelProvider: "openai.chat", FinishReason: finish, Performance: LanguageModelCallPerformance{ResponseTimeMs: ms}})
}

// TS 59465581b0 "exports seconds and model attributes": one point per model
// call, in seconds, with the chat attributes and no error.type, on the
// gen_ai meter with the semconv buckets.
func TestR85carryInferenceDurationRecorded(t *testing.T) {
	mp := &r85carryMeterProvider{}
	c := r85carryStart(t, mp, "ai.generateText")
	c.callStart()
	c.callEnd(1234, "stop", "response-model")
	c.i.OnStepEnd(c.ctx, TelemetryStepEndEvent{Settings: c.set, CallID: c.id, FinishReason: "stop"})
	c.i.OnEnd(c.ctx, TelemetryFinishEvent{Settings: c.set, CallID: c.id})

	points := mp.recorded()
	if len(points) != 1 || math.Abs(points[0].value-1.234) > 1e-9 {
		t.Fatalf("points = %+v, want one 1.234s point", points)
	}
	want := map[string]string{"gen_ai.operation.name": "chat", "gen_ai.provider.name": "openai", "gen_ai.request.model": "gpt-4", "gen_ai.response.model": "response-model"}
	if !reflect.DeepEqual(points[0].attrs, want) {
		t.Fatalf("attrs = %v, want %v", points[0].attrs, want)
	}
	if mp.meters[0] != "gen_ai" || mp.names[0] != "gen_ai.client.inference.duration" || mp.hists[0].Unit() != "s" ||
		!reflect.DeepEqual(mp.hists[0].ExplicitBucketBoundaries(), []float64{0.01, 0.02, 0.04, 0.08, 0.16, 0.32, 0.64, 1.28, 2.56, 5.12, 10.24, 20.48, 40.96, 81.92}) {
		t.Fatalf("instrument = %v %v %+v", mp.meters, mp.names, mp.hists)
	}
}

// TS resolves the global meter at record time: a MeterProvider registered
// after the integration was built receives the point.
func TestR85carryInferenceDurationGlobalMeterAtRecordTime(t *testing.T) {
	prev := otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetMeterProvider(prev) })
	c := r85carryStart(t, nil, "ai.streamText")
	c.callStart()
	mp := &r85carryMeterProvider{}
	otel.SetMeterProvider(mp)
	c.callEnd(10, "stop", "gpt-4")
	if points := mp.recorded(); len(points) != 1 {
		t.Fatalf("points = %+v, want 1", points)
	}
}

// TS: a stream that closes after an error chunk records `_OTHER` at step
// end; a retried attempt records the failed one as `_OTHER`; abort, step
// error and error record their error type; each call is recorded once.
func TestR85carryInferenceDurationErrorTypes(t *testing.T) {
	t.Run("step end after error", func(t *testing.T) {
		mp := &r85carryMeterProvider{}
		c := r85carryStart(t, mp, "ai.streamText")
		c.callStart()
		c.i.OnStepEnd(c.ctx, TelemetryStepEndEvent{Settings: c.set, CallID: c.id, FinishReason: "error"})
		c.i.OnEnd(c.ctx, TelemetryFinishEvent{Settings: c.set, CallID: c.id})
		points := mp.recorded()
		if len(points) != 1 || points[0].attrs["error.type"] != "_OTHER" {
			t.Fatalf("points = %+v", points)
		}
		if _, ok := points[0].attrs["gen_ai.response.model"]; ok {
			t.Fatalf("response model recorded without a model-call end: %v", points[0].attrs)
		}
	})
	t.Run("retried attempt", func(t *testing.T) {
		mp := &r85carryMeterProvider{}
		c := r85carryStart(t, mp, "ai.streamText")
		c.callStart()
		c.callStart()
		c.callEnd(750, "stop", "gpt-4")
		points := mp.recorded()
		if len(points) != 2 || points[0].attrs["error.type"] != "_OTHER" || points[1].attrs["error.type"] != "" || math.Abs(points[1].value-0.75) > 1e-9 {
			t.Fatalf("points = %+v", points)
		}
	})
	t.Run("finish reason error", func(t *testing.T) {
		mp := &r85carryMeterProvider{}
		c := r85carryStart(t, mp, "ai.generateText")
		c.callStart()
		c.callEnd(5, "error", "gpt-4")
		c.i.OnStepEnd(c.ctx, TelemetryStepEndEvent{Settings: c.set, CallID: c.id, FinishReason: "error"})
		if points := mp.recorded(); len(points) != 1 || points[0].attrs["error.type"] != "_OTHER" {
			t.Fatalf("points = %+v", points)
		}
	})
	for name, tc := range map[string]struct {
		fire func(c *r85carryCall)
		want string
	}{
		"abort": {func(c *r85carryCall) { c.i.OnAbort(c.ctx, TelemetryAbortEvent{CallID: c.id}) }, "AbortError"},
		"error": {func(c *r85carryCall) {
			c.i.OnError(c.ctx, TelemetryErrorEvent{CallID: c.id, Error: errors.New("boom")})
		}, "Error"},
		"step error api error": {func(c *r85carryCall) {
			c.i.OnStepError(c.ctx, TelemetryErrorEvent{CallID: c.id, Error: &providererrors.ProviderError{Message: "x"}})
		}, "AI_APICallError"},
		"error canceled": {func(c *r85carryCall) { c.i.OnError(c.ctx, TelemetryErrorEvent{CallID: c.id, Error: context.Canceled}) }, "AbortError"},
	} {
		t.Run(name, func(t *testing.T) {
			mp := &r85carryMeterProvider{}
			c := r85carryStart(t, mp, "ai.streamText")
			c.callStart()
			tc.fire(c)
			points := mp.recorded()
			if len(points) != 1 || points[0].attrs["error.type"] != tc.want {
				t.Fatalf("points = %+v, want one %s", points, tc.want)
			}
		})
	}
}

// TS records the histogram for generateText/streamText model calls only:
// generateObject's chat span (onObjectStepStart) records none.
func TestR85carryInferenceDurationNotForObjects(t *testing.T) {
	mp := &r85carryMeterProvider{}
	c := r85carryStart(t, mp, "ai.generateObject")
	c.callStart()
	c.callEnd(5, "stop", "gpt-4")
	if points := mp.recorded(); len(points) != 0 {
		t.Fatalf("points = %+v, want none", points)
	}
}
