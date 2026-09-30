package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	retryutil "github.com/digitallysavvy/go-ai/pkg/internal/retry"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// withEmbedRetry runs fn with the TS retry policy used by embed/embedMany/
// rerank: exponential backoff starting at 2s (factor 2), respecting
// retry-after(-ms) headers, retrying only retryable provider errors
// (HTTP 408/409/429/5xx). maxRetries <= 0 runs fn once.
func withEmbedRetry(ctx context.Context, maxRetries int, fn func(ctx context.Context) error) error {
	if maxRetries <= 0 {
		return fn(ctx)
	}
	return retryutil.Do(ctx, retryutil.Config{
		MaxRetries:   maxRetries,
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2,
		Jitter:       false,
		ShouldRetry:  isGatewayCallRetryable,
	}, fn)
}

// embedManyCalls performs the model calls of EmbedMany (split, parallelized,
// retried and validated) and aggregates the results. Mirrors the body of the
// TS embedMany try-block.
func embedManyCalls(ctx context.Context, opts EmbedManyOptions, callID string, maxRetries int) (*EmbedManyResult, error) {
	model := opts.Model
	maxEmbeddingsPerCall := model.MaxEmbeddingsPerCall()
	maxInputBytesPerCall := 0
	if m, ok := model.(provider.EmbeddingModelMaxInputBytesPerCall); ok {
		maxInputBytesPerCall = m.MaxInputBytesPerCall()
	}
	hasEmbeddingLimit := maxEmbeddingsPerCall > 0
	hasInputByteLimit := maxInputBytesPerCall > 0

	callModel := func(callCtx context.Context, values []string, providerOptions map[string]interface{}) (*types.EmbeddingsResult, error) {
		var result *types.EmbeddingsResult
		err := withEmbedRetry(callCtx, maxRetries, func(attemptCtx context.Context) error {
			embedCallID := newCallID()
			telemetry.FireOnEmbedStart(attemptCtx, telemetry.EmbeddingModelCallStartEvent{
				Settings:      opts.ExperimentalTelemetry,
				CallID:        callID,
				EmbedCallID:   embedCallID,
				OperationID:   "ai.embedMany.doEmbed",
				ModelProvider: model.Provider(),
				ModelID:       model.ModelID(),
				Values:        values,
			})
			res, err := model.DoEmbedMany(attemptCtx, values, &provider.EmbedModelOptions{
				ProviderOptions: providerOptions,
				Headers:         opts.Headers,
			})
			if err != nil {
				wrappedErr := fmt.Errorf("batch embedding failed: %w", err)
				// Close THIS attempt's span immediately, with error status, so
				// a later retry attempt's success doesn't leave it open forever
				// (OnEnd never sweeps leftover per-attempt spans — see the doc
				// comment on EmbeddingModelCallEndEvent).
				telemetry.FireOnEmbedEnd(attemptCtx, telemetry.EmbeddingModelCallEndEvent{
					Settings:      opts.ExperimentalTelemetry,
					CallID:        callID,
					EmbedCallID:   embedCallID,
					OperationID:   "ai.embedMany.doEmbed",
					ModelProvider: model.Provider(),
					ModelID:       model.ModelID(),
					Values:        values,
					Error:         wrappedErr,
				})
				return wrappedErr
			}
			if res == nil {
				res = &types.EmbeddingsResult{}
			}
			telemetry.FireOnEmbedEnd(attemptCtx, telemetry.EmbeddingModelCallEndEvent{
				Settings:      opts.ExperimentalTelemetry,
				CallID:        callID,
				EmbedCallID:   embedCallID,
				OperationID:   "ai.embedMany.doEmbed",
				ModelProvider: model.Provider(),
				ModelID:       model.ModelID(),
				Values:        values,
				Embeddings:    res.Embeddings,
				Usage:         res.Usage,
			})
			result = res
			return nil
		})
		if err != nil {
			return nil, err
		}
		// TS 0f2281e: reject responses whose count does not match the values.
		if err := validateEmbeddingCount(result.Embeddings, values); err != nil {
			return nil, err
		}
		return result, nil
	}

	if !hasEmbeddingLimit && !hasInputByteLimit {
		result, err := callModel(ctx, opts.Inputs, opts.ProviderOptions)
		if err != nil {
			return nil, err
		}
		return &EmbedManyResult{
			Embeddings:       result.Embeddings,
			Usage:            result.Usage,
			Warnings:         warningsOrEmpty(result.Warnings),
			ProviderMetadata: result.ProviderMetadata,
			Responses:        embeddingResponsesOrPlaceholder(result.Responses),
		}, nil
	}

	valueChunks := splitByEmbeddingLimits(opts.Inputs, maxEmbeddingsPerCall, maxInputBytesPerCall)
	transformer, _ := model.(provider.EmbeddingModelProviderOptionsTransformer)

	parallelism := 1
	if model.SupportsParallelCalls() {
		parallelism = opts.MaxParallelCalls
		if parallelism < 0 {
			return nil, &providererrors.InvalidArgumentError{
				Field:   "chunkSize",
				Message: "chunkSize must be greater than 0",
			}
		}
		if parallelism == 0 {
			parallelism = len(valueChunks) // TS default: Infinity
		}
	}

	out := &EmbedManyResult{
		Embeddings: make([][]float64, 0, len(opts.Inputs)),
		Warnings:   []types.Warning{},
		Responses:  []types.EmbeddingResponse{},
	}

	nextStartIndex := 0
	for groupStart := 0; groupStart < len(valueChunks); groupStart += parallelism {
		groupEnd := groupStart + parallelism
		if groupEnd > len(valueChunks) {
			groupEnd = len(valueChunks)
		}
		group := valueChunks[groupStart:groupEnd]

		// Resolve per-batch provider options before any call of the group (and
		// before the retry loop) so value/content alignment is fixed per batch
		// and invalid alignment fails before requests are sent.
		groupOptions := make([]map[string]interface{}, len(group))
		for i, chunk := range group {
			startIndex := nextStartIndex
			nextStartIndex += len(chunk)
			groupOptions[i] = opts.ProviderOptions
			if transformer != nil {
				transformed, err := transformer.TransformEmbeddingProviderOptions(ctx, provider.EmbeddingProviderOptionsTransformInput{
					ProviderOptions: opts.ProviderOptions,
					Values:          opts.Inputs,
					StartIndex:      startIndex,
					EndIndex:        startIndex + len(chunk),
				})
				if err != nil {
					return nil, err
				}
				groupOptions[i] = transformed
			}
		}

		results := make([]*types.EmbeddingsResult, len(group))
		errs := make([]error, len(group))
		if len(group) == 1 {
			results[0], errs[0] = callModel(ctx, group[0], groupOptions[0])
		} else {
			var wg sync.WaitGroup
			for i := range group {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					results[i], errs[i] = callModel(ctx, group[i], groupOptions[i])
				}(i)
			}
			wg.Wait()
		}
		if err := firstNonNil(errs); err != nil {
			return nil, err
		}

		for _, result := range results {
			out.Embeddings = append(out.Embeddings, result.Embeddings...)
			out.Warnings = append(out.Warnings, result.Warnings...)
			out.Responses = append(out.Responses, embeddingResponsesOrPlaceholder(result.Responses)...)
			out.Usage.Tokens += result.Usage.Tokens
			out.Usage.InputTokens += result.Usage.InputTokens
			out.Usage.TotalTokens += result.Usage.TotalTokens
			out.ProviderMetadata = mergeEmbeddingProviderMetadata(out.ProviderMetadata, result.ProviderMetadata)
		}
	}

	return out, nil
}

func firstNonNil(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// embeddingResponsesOrPlaceholder keeps one response entry per model call
// (TS pushes `response` even when a provider returns none).
func embeddingResponsesOrPlaceholder(responses []types.EmbeddingResponse) []types.EmbeddingResponse {
	if len(responses) == 0 {
		return []types.EmbeddingResponse{{}}
	}
	return responses
}

// mergeEmbeddingProviderMetadata shallow-merges metadata per provider key,
// later calls overriding earlier ones (TS embedMany chunked path).
func mergeEmbeddingProviderMetadata(dst, src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]interface{}, len(src))
	}
	for providerName, metadata := range src {
		srcMap, srcOK := metadata.(map[string]interface{})
		dstMap, dstOK := dst[providerName].(map[string]interface{})
		if !srcOK || !dstOK {
			if srcOK {
				cp := make(map[string]interface{}, len(srcMap))
				for k, v := range srcMap {
					cp[k] = v
				}
				dst[providerName] = cp
			} else {
				dst[providerName] = metadata
			}
			continue
		}
		merged := make(map[string]interface{}, len(dstMap)+len(srcMap))
		for k, v := range dstMap {
			merged[k] = v
		}
		for k, v := range srcMap {
			merged[k] = v
		}
		dst[providerName] = merged
	}
	return dst
}

func validateEmbeddingCount(embeddings [][]float64, values []string) error {
	if len(embeddings) != len(values) {
		data := embeddings
		if data == nil {
			data = [][]float64{}
		}
		return providererrors.NewInvalidResponseDataError(data,
			fmt.Sprintf("Expected %d embeddings, but received %d.", len(values), len(embeddings)))
	}
	return nil
}

// splitByEmbeddingLimits splits values into batches that respect both the
// per-call embedding count and the cumulative UTF-8 byte budget. A single
// value larger than the byte budget is sent by itself. Limits <= 0 mean
// "unlimited" (Go interface convention; TS uses Infinity).
func splitByEmbeddingLimits(values []string, maxEmbeddingsPerCall, maxInputBytesPerCall int) [][]string {
	if len(values) == 0 {
		return [][]string{}
	}
	var chunks [][]string
	var current []string
	currentBytes := 0
	for _, value := range values {
		inputBytes := len(value) // Go strings are UTF-8 encoded
		if len(current) > 0 &&
			((maxEmbeddingsPerCall > 0 && len(current) >= maxEmbeddingsPerCall) ||
				(maxInputBytesPerCall > 0 && currentBytes+inputBytes > maxInputBytesPerCall)) {
			chunks = append(chunks, current)
			current = nil
			currentBytes = 0
		}
		current = append(current, value)
		currentBytes += inputBytes
	}
	return append(chunks, current)
}
