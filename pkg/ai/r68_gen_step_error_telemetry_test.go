package ai

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r68StepErrorTelemetry struct {
	telemetry.NoopTelemetryIntegration
	mu        sync.Mutex
	stepErr   error
	rootErr   error
	stepCalls int
}

func (r *r68StepErrorTelemetry) OnStepError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stepCalls++
	r.stepErr = e.Error
}

func (r *r68StepErrorTelemetry) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rootErr = e.Error
}

func (r *r68StepErrorTelemetry) check(t *testing.T, authErr error) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stepCalls != 1 || r.stepErr == nil || !strings.Contains(r.stepErr.Error(), "Unauthenticated request to AI Gateway") {
		t.Fatalf("telemetry OnStepError = %d call(s) with %v, want one with the wrapped gateway error", r.stepCalls, r.stepErr)
	}
	if r.rootErr != authErr {
		t.Fatalf("telemetry OnError = %v, want the original error", r.rootErr)
	}
}

// TS generateText and streamText notify telemetry onStepError with
// wrapGatewayError(error) and telemetry onError with the original error.
func TestR68StepErrorTelemetryGetsWrappedGatewayError(t *testing.T) {
	t.Setenv("NODE_ENV", "")
	authErr := gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
	failing := func() *testutil.MockLanguageModel {
		return &testutil.MockLanguageModel{
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return nil, authErr
			},
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return nil, authErr
			},
		}
	}

	t.Run("generate", func(t *testing.T) {
		rec := &r68StepErrorTelemetry{}
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: failing(), Prompt: "q",
			Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}},
		})
		if err == nil {
			t.Fatal("expected an error")
		}
		rec.check(t, authErr)
	})

	t.Run("stream", func(t *testing.T) {
		rec := &r68StepErrorTelemetry{}
		res, err := StreamText(context.Background(), StreamTextOptions{
			Model: failing(), Prompt: "q",
			Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}},
		})
		if err == nil {
			_, _ = res.ReadAll()
		}
		rec.check(t, authErr)
	})
}

// TS generateText notifies telemetry onStepError for every failure of a
// started step (here a tool choice violation after the model call), not only
// for a failed model call.
func TestR68GenerateTextStepErrorTelemetryAfterModelCall(t *testing.T) {
	rec := &r68StepErrorTelemetry{}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "x", FinishReason: types.FinishReasonStop}, nil
		}},
		Prompt: "q", ToolChoice: types.RequiredToolChoice(),
		Tools:     []types.Tool{{Name: "a"}},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if !IsToolChoiceViolationError(err) {
		t.Fatalf("err = %v, want a tool choice violation", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.stepCalls != 1 || !IsToolChoiceViolationError(rec.stepErr) || rec.rootErr == nil {
		t.Fatalf("telemetry OnStepError = %d call(s) (%v), OnError = %v; want one step error, then the root error", rec.stepCalls, rec.stepErr, rec.rootErr)
	}
}
