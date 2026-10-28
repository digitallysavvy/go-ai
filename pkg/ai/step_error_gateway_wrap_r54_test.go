package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generateText notifies onStepError with the wrapGatewayError'd error, as
// it rethrows it.
func TestGenerateTextOnStepErrorGetsWrappedGatewayError(t *testing.T) {
	t.Setenv("NODE_ENV", "production")
	const want = "Unauthenticated. Configure AI_GATEWAY_API_KEY or use a provider module. Learn more: https://ai-sdk.dev/unauthenticated-ai-gateway"
	zero := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
		},
	}
	var stepErr error
	_, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", MaxRetries: &zero,
		OnStepError: func(_ context.Context, e OnStepErrorEvent) { stepErr = e.Error }})
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
	if stepErr == nil || stepErr.Error() != want {
		t.Fatalf("OnStepError error = %v, want the wrapped gateway error", stepErr)
	}
}
