package tools

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type Advisor20260301Caching struct {
	Type string `json:"type"`
	TTL  string `json:"ttl"`
}

type Advisor20260301Args struct {
	Model   string                  `json:"model"`
	MaxUses *int                    `json:"maxUses,omitempty"`
	Caching *Advisor20260301Caching `json:"caching,omitempty"`
}

type advisor20260301Options struct {
	Model   string                  `json:"model"`
	MaxUses *int                    `json:"max_uses,omitempty"`
	Caching *Advisor20260301Caching `json:"caching,omitempty"`
}

func (a advisor20260301Options) ToAnthropicAPIMap() map[string]interface{} {
	m := map[string]interface{}{
		"type":  "advisor_20260301",
		"name":  "advisor",
		"model": a.Model,
	}
	if a.MaxUses != nil {
		m["max_uses"] = *a.MaxUses
	}
	if a.Caching != nil {
		m["caching"] = map[string]interface{}{
			"type": a.Caching.Type,
			"ttl":  a.Caching.TTL,
		}
	}
	return m
}

func Advisor20260301(args Advisor20260301Args) types.Tool {
	if args.Model == "" {
		args.Model = "claude-opus-4-7"
	}
	if args.Caching != nil && args.Caching.Type == "" {
		args.Caching.Type = "ephemeral"
	}
	return types.Tool{
		Name:            "anthropic.advisor_20260301",
		Description:     "Anthropic advisor tool for advisory sub-inference with optional per-request limits and caching.",
		Parameters:      map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		ProviderOptions: advisor20260301Options{Model: args.Model, MaxUses: args.MaxUses, Caching: args.Caching},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("advisor tool must be executed by the provider (Anthropic). Set ProviderExecuted: true")
		},
		ProviderExecuted:        true,
		SupportsDeferredResults: true,
	}
}
