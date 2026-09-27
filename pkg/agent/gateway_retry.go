package agent

import (
	"context"
	"errors"
	"time"

	retryutil "github.com/digitallysavvy/go-ai/pkg/internal/retry"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// gatewayMaxRetriesForStep mirrors pkg/ai's gatewayMaxRetries: retries only
// apply to AI Gateway models, matching the TypeScript SDK's Gateway-specific
// fallback retry behavior. Needed so agent.AgentConfig.MaxRetries (WORKFLOW-
// AGENT-OPTIONS, e6064c5) has an effect on GenerateAgent's own step loop,
// which — unlike Generate/Stream — calls model.DoGenerate directly instead of
// delegating to ai.GenerateText/StreamText's retry wrapper.
func gatewayMaxRetriesForStep(model provider.LanguageModel, maxRetries *int) int {
	if model == nil || model.Provider() != "gateway" {
		return 0
	}
	if maxRetries == nil {
		return 2
	}
	return *maxRetries
}

func isGatewayStepCallRetryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var gatewayErr gatewayerrors.GatewayError
	if errors.As(err, &gatewayErr) {
		return gatewayErr.IsRetryable()
	}
	var providerErr *providererrors.ProviderError
	if errors.As(err, &providerErr) {
		status := providerErr.StatusCode
		return status == 408 || status == 409 || status == 429 || status >= 500
	}
	return false
}

// doGenerateWithGatewayRetry retries a step's model.DoGenerate call for
// Gateway models, mirroring pkg/ai's private doGenerateWithGatewayRetry.
func doGenerateWithGatewayRetry(ctx context.Context, model provider.LanguageModel, opts *provider.GenerateOptions, maxRetries *int) (*types.GenerateResult, error) {
	retries := gatewayMaxRetriesForStep(model, maxRetries)
	if retries <= 0 {
		return model.DoGenerate(ctx, opts)
	}
	var result *types.GenerateResult
	err := retryutil.Do(ctx, retryutil.Config{
		MaxRetries:   retries,
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2,
		Jitter:       false,
		ShouldRetry:  isGatewayStepCallRetryable,
	}, func(retryCtx context.Context) error {
		var err error
		result, err = model.DoGenerate(retryCtx, opts)
		return err
	})
	return result, err
}
