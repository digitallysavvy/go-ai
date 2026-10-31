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
	Model   string `json:"model"`
	MaxUses *int   `json:"maxUses,omitempty"`
	// MaxTokens is the maximum number of tokens the advisor can generate per
	// call, including thinking and text. Minimum 1024. Serialized as
	// max_tokens (TS advisor_20260301.ts).
	MaxTokens *int                    `json:"maxTokens,omitempty"`
	Caching   *Advisor20260301Caching `json:"caching,omitempty"`
}

type advisor20260301Options struct {
	Model     string                  `json:"model"`
	MaxUses   *int                    `json:"max_uses,omitempty"`
	MaxTokens *int                    `json:"max_tokens,omitempty"`
	Caching   *Advisor20260301Caching `json:"caching,omitempty"`
}

// Validate mirrors advisor_20260301ArgsSchema: maxTokens is at least 1024 and
// caching carries an ephemeral type with a 5m or 1h ttl.
func (a advisor20260301Options) Validate() error {
	if a.MaxTokens != nil && *a.MaxTokens < 1024 {
		return fmt.Errorf("advisor_20260301 maxTokens must be an integer of at least 1024, got %d", *a.MaxTokens)
	}
	if a.Caching != nil {
		if a.Caching.Type != "ephemeral" {
			return fmt.Errorf("advisor_20260301 caching.type must be \"ephemeral\", got %q", a.Caching.Type)
		}
		if a.Caching.TTL != "5m" && a.Caching.TTL != "1h" {
			return fmt.Errorf("advisor_20260301 caching.ttl must be \"5m\" or \"1h\", got %q", a.Caching.TTL)
		}
	}
	return nil
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
	if a.MaxTokens != nil {
		m["max_tokens"] = *a.MaxTokens
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
		Type:            types.ToolTypeProviderDefined,
		ProviderID:      "anthropic.advisor_20260301",
		OutputSchema:    toolOutputSchema("anthropic.advisor_20260301"),
		ProviderArgs:    advisor20260301ProviderArgs(args),
		Description:     "Anthropic advisor tool for advisory sub-inference with optional per-request limits and caching.",
		Parameters:      map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		ProviderOptions: advisor20260301Options(args),
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("advisor tool must be executed by the provider (Anthropic). Set ProviderExecuted: true")
		},
		ProviderExecuted:        true,
		SupportsDeferredResults: true,
	}
}
