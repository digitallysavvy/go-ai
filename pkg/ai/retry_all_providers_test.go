package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func fastRetryErr(status int) *providererrors.ProviderError {
	e := providererrors.NewProviderError("acme", status, "", "busy", nil)
	e.ResponseHeaders = map[string]string{"retry-after-ms": "1"}
	return e
}

// TS retries every model (default 2 retries), not just the AI Gateway.
func TestGenerateText_RetriesNonGatewayProviderByDefault(t *testing.T) {
	t.Parallel()
	calls := 0
	model := &testutil.MockLanguageModel{
		ProviderName: "acme",
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls < 3 {
				return nil, fastRetryErr(503)
			}
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi"})
	if err != nil || res.Text != "ok" || calls != 3 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestGenerateText_NonGatewayRetryExhaustedWrapsInRetryError(t *testing.T) {
	t.Parallel()
	calls := 0
	model := &testutil.MockLanguageModel{
		ProviderName: "acme",
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			return nil, fastRetryErr(429)
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi"})
	var re *providererrors.RetryError
	if !errors.As(err, &re) || calls != 3 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestGenerateText_ProviderRetryableOverrideIsHonoured(t *testing.T) {
	t.Parallel()
	no := false
	calls := 0
	model := &testutil.MockLanguageModel{
		ProviderName: "acme",
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			e := fastRetryErr(503)
			e.Retryable = &no
			return nil, e
		},
	}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi"}); err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	yes := true
	calls = 0
	model.DoGenerateFunc = func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
		calls++
		if calls == 1 {
			e := fastRetryErr(0)
			e.Retryable = &yes
			return nil, e
		}
		return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
	}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi"}); err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestStreamText_RetriesNonGatewayProviderByDefault(t *testing.T) {
	t.Parallel()
	calls := 0
	model := &testutil.MockLanguageModel{
		ProviderName: "acme",
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			if calls < 2 {
				return nil, fastRetryErr(500)
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil || res.Text() != "ok" || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

type connectFailure struct{}

func (connectFailure) Error() string     { return "Cannot connect to API: dial tcp: refused" }
func (connectFailure) IsRetryable() bool { return true }

func TestGenerateText_RetriesSelfClassifiedRetryableErrors(t *testing.T) {
	t.Parallel()
	calls := 0
	model := &testutil.MockLanguageModel{
		ProviderName: "acme",
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return nil, connectFailure{}
			}
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi"}); err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
