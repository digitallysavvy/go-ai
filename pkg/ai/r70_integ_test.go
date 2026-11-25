package ai

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

var r70IntegSchema = schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

// gen CR (r70): a model whose DoGenerate returns (nil, nil) fails
// GenerateObject (TS reads the response right after doGenerate) instead of
// panicking, with and without retries; GenerateObjectInto goes through the
// same path.
func TestR70Integ_GenerateObject_NilModelResultIsAnError(t *testing.T) {
	for _, retries := range []int{0, 2} {
		model := &testutil.MockLanguageModel{
			ProviderName:      "p",
			ModelName:         "m",
			StructuredSupport: true,
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return nil, nil
			},
		}
		n := retries
		_, err := GenerateObject(context.Background(), GenerateObjectOptions{
			Model: model, Prompt: "p", MaxRetries: &n,
			Schema: r70IntegSchema,
		})
		if err == nil || !strings.Contains(err.Error(), "language model p/m returned no result") {
			t.Fatalf("retries=%d: err = %v, want the no-result error", retries, err)
		}
	}
}

// StreamObject already reported a nil stream (v0.5.2 "stream is nil"); the
// text is kept.
func TestR70Integ_StreamObject_NilModelStreamIsAnError(t *testing.T) {
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, nil
		},
	}
	zero := 0
	_, err := StreamObject(context.Background(), StreamObjectOptions{Model: model, Prompt: "p", Schema: r70IntegSchema, MaxRetries: &zero})
	if err == nil || !strings.Contains(err.Error(), "stream is nil") {
		t.Fatalf("err = %v, want stream is nil", err)
	}
}

// r70OrderTelemetry records the telemetry step-end and end calls in the
// shared order log.
type r70OrderTelemetry struct {
	telemetry.NoopTelemetryIntegration
	log *r70OrderLog
}

func (r *r70OrderTelemetry) OnStepEnd(context.Context, telemetry.TelemetryStepEndEvent) {
	r.log.add("telemetry.onStepEnd")
}

func (r *r70OrderTelemetry) OnFinish(context.Context, telemetry.TelemetryFinishEvent) {
	r.log.add("telemetry.onEnd")
}

type r70OrderLog struct {
	mu  sync.Mutex
	got []string
}

func (l *r70OrderLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = append(l.got, s)
}

func (l *r70OrderLog) check(t *testing.T, want []string) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if strings.Join(l.got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", l.got, want)
	}
}

// Coordinator ruling (r70): TS notify([onStepFinish, telemetry.onStepEnd])
// and notify([onEnd, telemetry.onEnd]) start the user callback first, so the
// telemetry integration's step-end and end run after the callbacks, in
// GenerateText and StreamText.
func TestR70Integ_CallbacksRunBeforeTelemetryStepEndAndEnd(t *testing.T) {
	want := []string{"onStepEnd", "onStepEndEvent", "telemetry.onStepEnd", "onEnd", "onEndEvent", "telemetry.onEnd"}

	log := &r70OrderLog{}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}, nil
		},
	}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "p",
		Telemetry:      &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{&r70OrderTelemetry{log: log}}},
		OnStepEnd:      func(context.Context, types.StepResult, interface{}) { log.add("onStepEnd") },
		OnStepEndEvent: func(context.Context, OnStepFinishEvent) { log.add("onStepEndEvent") },
		OnEnd:          func(context.Context, *GenerateTextResult, interface{}) { log.add("onEnd") },
		OnEndEvent:     func(context.Context, OnFinishEvent) { log.add("onEndEvent") },
	}); err != nil {
		t.Fatal(err)
	}
	log.check(t, want)

	log = &r70OrderLog{}
	smodel := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: smodel, Prompt: "p",
		Telemetry:      &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{&r70OrderTelemetry{log: log}}},
		OnStepEnd:      func(context.Context, types.StepResult, interface{}) { log.add("onStepEnd") },
		OnStepEndEvent: func(context.Context, OnStepFinishEvent) { log.add("onStepEndEvent") },
		OnEnd:          func(*StreamTextResult) { log.add("onEnd") },
		OnEndEvent:     func(context.Context, OnFinishEvent) { log.add("onEndEvent") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	log.check(t, want)
}
