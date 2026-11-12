package gateway

import (
	"context"
	"fmt"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// GenerationInfoParams identifies a generation to retrieve.
type GenerationInfoParams struct {
	ID string
}

// GenerationInfo contains detailed information about a single generation.
type GenerationInfo struct {
	ID           string  `json:"id"`
	Model        string  `json:"model"`
	ProviderName string  `json:"providerName"`
	FinishReason string  `json:"finishReason"`
	TotalCost    float64 `json:"totalCost"`
	// MarketCost is the market cost in USD, excluding surcharges; nil when
	// unavailable (TS marketCost?, 32acf432db).
	MarketCost *float64 `json:"marketCost,omitempty"`
	// SurchargeCost is the total of the surcharges applied in USD; nil when
	// unavailable (TS surchargeCost?).
	SurchargeCost *float64 `json:"surchargeCost,omitempty"`
	// GatewayCost is the total amount debited in USD; nil when unavailable
	// (TS gatewayCost?).
	GatewayCost            *float64 `json:"gatewayCost,omitempty"`
	UpstreamInferenceCost  float64  `json:"upstreamInferenceCost"`
	Usage                  float64  `json:"usage"`
	IsByok                 bool     `json:"isByok"`
	Streamed               bool     `json:"streamed"`
	CreatedAt              string   `json:"createdAt"`
	PromptTokens           int64    `json:"promptTokens"`
	CompletionTokens       int64    `json:"completionTokens"`
	ReasoningTokens        int64    `json:"reasoningTokens"`
	CachedTokens           int64    `json:"cachedTokens"`
	CacheCreationTokens    int64    `json:"cacheCreationTokens"`
	Latency                int64    `json:"latency"`
	GenerationTime         int64    `json:"generationTime"`
	BillableWebSearchCalls int      `json:"billableWebSearchCalls"`
}

type generationInfoEnvelope struct {
	Data generationInfoWire `json:"data"`
}

type generationInfoWire struct {
	ID                    string   `json:"id"`
	Model                 string   `json:"model"`
	ProviderName          string   `json:"provider_name"`
	FinishReason          string   `json:"finish_reason"`
	TotalCost             float64  `json:"total_cost"`
	MarketCost            *float64 `json:"market_cost"`
	SurchargeCost         *float64 `json:"surcharge_cost"`
	GatewayCost           *float64 `json:"gateway_cost"`
	UpstreamInferenceCost float64  `json:"upstream_inference_cost"`
	Usage                 float64  `json:"usage"`
	IsByok                bool     `json:"is_byok"`
	Streamed              bool     `json:"streamed"`
	CreatedAt             string   `json:"created_at"`
	// The counts and times are z.number() in TS: decoded as float64 so a
	// fractional value parses, then truncated into the v0.5.2 integer
	// fields (S1).
	PromptTokens           float64 `json:"native_tokens_prompt"`
	CompletionTokens       float64 `json:"native_tokens_completion"`
	ReasoningTokens        float64 `json:"native_tokens_reasoning"`
	CachedTokens           float64 `json:"native_tokens_cached"`
	CacheCreationTokens    float64 `json:"native_tokens_cache_creation"`
	Latency                float64 `json:"latency"`
	GenerationTime         float64 `json:"generation_time"`
	BillableWebSearchCalls float64 `json:"billable_web_search_calls"`
}

func (p *Provider) GetGenerationInfo(ctx context.Context, params GenerationInfoParams) (*GenerationInfo, error) {
	body, err := p.doOriginRequest(ctx, "/v1/generation?id="+providerutils.EncodeURIComponent(params.ID))
	if err != nil {
		return nil, err
	}

	if err := providerutils.MediaResponseValidator("gateway", providerutils.SchemaGatewayGenerationInfo)(http.StatusOK, http.Header{}, body); err != nil {
		return nil, p.handleErrorWithContext(ctx, err)
	}
	var envelope generationInfoEnvelope
	if err := jsonexact.Unmarshal(body, &envelope); err != nil {
		return nil, p.handleErrorWithContext(ctx, fmt.Errorf("failed to decode generation info response: %w", err))
	}

	return &GenerationInfo{
		ID:                     envelope.Data.ID,
		Model:                  envelope.Data.Model,
		ProviderName:           envelope.Data.ProviderName,
		FinishReason:           envelope.Data.FinishReason,
		TotalCost:              envelope.Data.TotalCost,
		MarketCost:             envelope.Data.MarketCost,
		SurchargeCost:          envelope.Data.SurchargeCost,
		GatewayCost:            envelope.Data.GatewayCost,
		UpstreamInferenceCost:  envelope.Data.UpstreamInferenceCost,
		Usage:                  envelope.Data.Usage,
		IsByok:                 envelope.Data.IsByok,
		Streamed:               envelope.Data.Streamed,
		CreatedAt:              envelope.Data.CreatedAt,
		PromptTokens:           int64(envelope.Data.PromptTokens),
		CompletionTokens:       int64(envelope.Data.CompletionTokens),
		ReasoningTokens:        int64(envelope.Data.ReasoningTokens),
		CachedTokens:           int64(envelope.Data.CachedTokens),
		CacheCreationTokens:    int64(envelope.Data.CacheCreationTokens),
		Latency:                int64(envelope.Data.Latency),
		GenerationTime:         int64(envelope.Data.GenerationTime),
		BillableWebSearchCalls: int(envelope.Data.BillableWebSearchCalls),
	}, nil
}
