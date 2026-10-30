package ai

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

const r68WrappedGatewayText = "Unauthenticated. Configure AI_GATEWAY_API_KEY or use a provider module. Learn more: https://ai-sdk.dev/unauthenticated-ai-gateway"

type r68StepErrorTelemetry struct {
	telemetry.NoopTelemetryIntegration
	mu         sync.Mutex
	stepErrors []error
	errs       []error
}

func (r *r68StepErrorTelemetry) OnStepError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	r.stepErrors = append(r.stepErrors, e.Error)
	r.mu.Unlock()
}

func (r *r68StepErrorTelemetry) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	r.errs = append(r.errs, e.Error)
	r.mu.Unlock()
}

// r68FailingStream yields one text delta, then fails.
type r68FailingStream struct {
	err  error
	sent bool
}

func (s *r68FailingStream) Next() (*provider.StreamChunk, error) {
	if !s.sent {
		s.sent = true
		return &provider.StreamChunk{Type: provider.ChunkTypeText, Text: "a"}, nil
	}
	return nil, s.err
}
func (s *r68FailingStream) Err() error   { return s.err }
func (s *r68FailingStream) Close() error { return nil }

func r68RunStream(t *testing.T, opts StreamTextOptions) (*r68StepErrorTelemetry, []error) {
	t.Helper()
	rec := &r68StepErrorTelemetry{}
	var mu sync.Mutex
	var userStepErrors []error
	opts.Prompt = "hi"
	opts.Telemetry = &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}}
	opts.OnStepError = func(_ context.Context, e OnStepErrorEvent) {
		mu.Lock()
		userStepErrors = append(userStepErrors, e.Error)
		mu.Unlock()
	}
	zero := 0
	opts.MaxRetries = &zero
	res, err := StreamText(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	stream := res.Stream()
	for {
		if _, err := stream.Next(); err != nil {
			break // io.EOF or the stream's error
		}
	}
	_ = res.Err()
	mu.Lock()
	defer mu.Unlock()
	return rec, append([]error(nil), userStepErrors...)
}

func r68ErrTexts(errs []error) []string {
	out := make([]string, len(errs))
	for i, err := range errs {
		if err == nil {
			out[i] = "<nil>"
		} else {
			out[i] = err.Error()
		}
	}
	return out
}

// TS streamText's outer stream catch runs notifyStepError(wrapGatewayError(
// error)) when the model stream fails mid-step: onStepError and the telemetry
// onStepError get the wrapped error, the telemetry onError the error itself.
// Go called neither telemetry onStepError nor wrapped the user's error.
func TestR68StreamTextMidStreamFailureNotifiesTelemetryStepError(t *testing.T) {
	t.Run("plain error", func(t *testing.T) {
		boom := errors.New("boom")
		rec, user := r68RunStream(t, StreamTextOptions{Model: &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return &r68FailingStream{err: boom}, nil
			},
		}})
		rec.mu.Lock()
		defer rec.mu.Unlock()
		if len(rec.stepErrors) != 1 || !errors.Is(rec.stepErrors[0], boom) {
			t.Fatalf("telemetry OnStepError = %v, want [boom]", r68ErrTexts(rec.stepErrors))
		}
		if len(user) != 1 || !errors.Is(user[0], boom) {
			t.Fatalf("OnStepError = %v, want [boom]", r68ErrTexts(user))
		}
	})
	t.Run("gateway auth error", func(t *testing.T) {
		t.Setenv("NODE_ENV", "production")
		authErr := gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
		rec, user := r68RunStream(t, StreamTextOptions{Model: &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return &r68FailingStream{err: authErr}, nil
			},
		}})
		rec.mu.Lock()
		defer rec.mu.Unlock()
		if got := r68ErrTexts(rec.stepErrors); len(got) != 1 || got[0] != r68WrappedGatewayText {
			t.Fatalf("telemetry OnStepError = %q, want the wrapped gateway error", got)
		}
		if got := r68ErrTexts(user); len(got) != 1 || got[0] != r68WrappedGatewayText {
			t.Fatalf("OnStepError = %q, want the wrapped gateway error", got)
		}
		if len(rec.errs) != 1 || rec.errs[0] != error(authErr) {
			t.Fatalf("telemetry OnError = %q, want the gateway error itself", r68ErrTexts(rec.errs))
		}
	})
}

// TS notifyStepError gives the telemetry onStepError wrapGatewayError(error)
// for a first step whose model call fails to open, as it gives onStepError.
func TestR68StreamTextOpenFailureTelemetryStepErrorWrapped(t *testing.T) {
	t.Setenv("NODE_ENV", "production")
	rec, user := r68RunStream(t, StreamTextOptions{Model: &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
		},
	}})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if got := r68ErrTexts(rec.stepErrors); len(got) != 1 || got[0] != r68WrappedGatewayText {
		t.Fatalf("telemetry OnStepError = %q, want the wrapped gateway error", got)
	}
	if got := r68ErrTexts(user); len(got) != 1 || got[0] != r68WrappedGatewayText {
		t.Fatalf("OnStepError = %q, want the wrapped gateway error", got)
	}
}

// A later step whose model call fails to open reports its step error once to
// each of onStepError and the telemetry onStepError.
func TestR68StreamTextLaterStepOpenFailureStepErrorOnce(t *testing.T) {
	t.Setenv("NODE_ENV", "production")
	calls := 0
	rec, user := r68RunStream(t, StreamTextOptions{
		StopWhen: []StopCondition{IsStepCount(3)},
		Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "r", nil
		}}},
		Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			if calls > 1 {
				return nil, gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c", ToolName: "t", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		}},
	})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if got := r68ErrTexts(rec.stepErrors); len(got) != 1 || got[0] != r68WrappedGatewayText {
		t.Fatalf("telemetry OnStepError = %q, want one wrapped gateway error", got)
	}
	if got := r68ErrTexts(user); len(got) != 1 || got[0] != r68WrappedGatewayText {
		t.Fatalf("OnStepError = %q, want one wrapped gateway error", got)
	}
}

// TS wrapGatewayError checks the error itself (GatewayAuthenticationError
// .isInstance), so wrapping an already wrapped error returns it unchanged.
func TestR68WrapGatewayErrorIdempotent(t *testing.T) {
	t.Setenv("NODE_ENV", "production")
	once := wrapGatewayError(gatewayerrors.NewGatewayAuthenticationError("", 401, nil, ""))
	if twice := wrapGatewayError(once); twice != once {
		t.Fatalf("wrapGatewayError(wrapped) = %#v, want the same error", twice)
	}
}

// GenerateText: TS notifyStepError gives the telemetry onStepError the same
// wrapGatewayError(error) as onStepError.
func TestR68GenerateTextTelemetryStepErrorWrapped(t *testing.T) {
	t.Setenv("NODE_ENV", "production")
	rec := &r68StepErrorTelemetry{}
	zero := 0
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
		}},
		Prompt:     "hi",
		MaxRetries: &zero,
		Telemetry:  &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if err == nil {
		t.Fatal("want an error")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if got := r68ErrTexts(rec.stepErrors); len(got) != 1 || got[0] != r68WrappedGatewayText {
		t.Fatalf("telemetry OnStepError = %q, want the wrapped gateway error", got)
	}
}
