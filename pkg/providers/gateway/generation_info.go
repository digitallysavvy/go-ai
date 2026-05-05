package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// GenerationInfoParams identifies a generation to retrieve.
type GenerationInfoParams struct {
	ID string
}

// GenerationInfo contains detailed information about a single generation.
type GenerationInfo struct {
	ID                     string  `json:"id"`
	Model                  string  `json:"model"`
	ProviderName           string  `json:"providerName"`
	FinishReason           string  `json:"finishReason"`
	TotalCost              float64 `json:"totalCost"`
	UpstreamInferenceCost  float64 `json:"upstreamInferenceCost"`
	Usage                  float64 `json:"usage"`
	IsByok                 bool    `json:"isByok"`
	Streamed               bool    `json:"streamed"`
	CreatedAt              string  `json:"createdAt"`
	PromptTokens           int64   `json:"promptTokens"`
	CompletionTokens       int64   `json:"completionTokens"`
	ReasoningTokens        int64   `json:"reasoningTokens"`
	CachedTokens           int64   `json:"cachedTokens"`
	CacheCreationTokens    int64   `json:"cacheCreationTokens"`
	Latency                int64   `json:"latency"`
	GenerationTime         int64   `json:"generationTime"`
	BillableWebSearchCalls int     `json:"billableWebSearchCalls"`
}

type generationInfoEnvelope struct {
	Data generationInfoWire `json:"data"`
}

type generationInfoWire struct {
	ID                     string  `json:"id"`
	Model                  string  `json:"model"`
	ProviderName           string  `json:"provider_name"`
	FinishReason           string  `json:"finish_reason"`
	TotalCost              float64 `json:"total_cost"`
	UpstreamInferenceCost  float64 `json:"upstream_inference_cost"`
	Usage                  float64 `json:"usage"`
	IsByok                 bool    `json:"is_byok"`
	Streamed               bool    `json:"streamed"`
	CreatedAt              string  `json:"created_at"`
	PromptTokens           int64   `json:"native_tokens_prompt"`
	CompletionTokens       int64   `json:"native_tokens_completion"`
	ReasoningTokens        int64   `json:"native_tokens_reasoning"`
	CachedTokens           int64   `json:"native_tokens_cached"`
	CacheCreationTokens    int64   `json:"native_tokens_cache_creation"`
	Latency                int64   `json:"latency"`
	GenerationTime         int64   `json:"generation_time"`
	BillableWebSearchCalls int     `json:"billable_web_search_calls"`
}

func (p *Provider) GetGenerationInfo(ctx context.Context, params GenerationInfoParams) (*GenerationInfo, error) {
	if params.ID == "" {
		return nil, fmt.Errorf("generation ID is required")
	}

	body, err := p.doOriginRequest(ctx, "/v1/generation?id="+url.QueryEscape(params.ID))
	if err != nil {
		return nil, err
	}

	var envelope generationInfoEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("failed to decode generation info response: %w", err)
	}

	return &GenerationInfo{
		ID:                     envelope.Data.ID,
		Model:                  envelope.Data.Model,
		ProviderName:           envelope.Data.ProviderName,
		FinishReason:           envelope.Data.FinishReason,
		TotalCost:              envelope.Data.TotalCost,
		UpstreamInferenceCost:  envelope.Data.UpstreamInferenceCost,
		Usage:                  envelope.Data.Usage,
		IsByok:                 envelope.Data.IsByok,
		Streamed:               envelope.Data.Streamed,
		CreatedAt:              envelope.Data.CreatedAt,
		PromptTokens:           envelope.Data.PromptTokens,
		CompletionTokens:       envelope.Data.CompletionTokens,
		ReasoningTokens:        envelope.Data.ReasoningTokens,
		CachedTokens:           envelope.Data.CachedTokens,
		CacheCreationTokens:    envelope.Data.CacheCreationTokens,
		Latency:                envelope.Data.Latency,
		GenerationTime:         envelope.Data.GenerationTime,
		BillableWebSearchCalls: envelope.Data.BillableWebSearchCalls,
	}, nil
}
