package middleware

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// DefaultSettingsMiddleware creates a language model middleware that applies default settings
// to all generate/stream calls. Settings provided in the call will override these defaults.
func DefaultSettingsMiddleware(settings *provider.GenerateOptions) *LanguageModelMiddleware {
	return &LanguageModelMiddleware{
		SpecificationVersion: "v3",
		TransformParams: func(ctx context.Context, callType string, params *provider.GenerateOptions, model provider.LanguageModel) (*provider.GenerateOptions, error) {
			// Merge settings with params, params take precedence
			merged := mergeGenerateOptions(settings, params)
			return merged, nil
		},
	}
}

// mergeGenerateOptions merges two GenerateOptions, with the second taking precedence
func mergeGenerateOptions(defaults, overrides *provider.GenerateOptions) *provider.GenerateOptions {
	if defaults == nil {
		return overrides
	}
	if overrides == nil {
		return defaults
	}

	result := &provider.GenerateOptions{}

	// Copy from defaults first
	if defaults.Prompt.Messages != nil {
		result.Prompt.Messages = defaults.Prompt.Messages
	}
	if defaults.MaxTokens != nil {
		result.MaxTokens = defaults.MaxTokens
	}
	if defaults.Temperature != nil {
		result.Temperature = defaults.Temperature
	}
	if defaults.TopP != nil {
		result.TopP = defaults.TopP
	}
	if defaults.TopK != nil {
		result.TopK = defaults.TopK
	}
	if defaults.PresencePenalty != nil {
		result.PresencePenalty = defaults.PresencePenalty
	}
	if defaults.FrequencyPenalty != nil {
		result.FrequencyPenalty = defaults.FrequencyPenalty
	}
	if defaults.StopSequences != nil {
		result.StopSequences = defaults.StopSequences
	}
	if defaults.Seed != nil {
		result.Seed = defaults.Seed
	}
	if defaults.Tools != nil {
		result.Tools = defaults.Tools
	}
	if defaults.ToolChoice.Type != "" {
		result.ToolChoice = defaults.ToolChoice
	}
	if defaults.ResponseFormat != nil {
		result.ResponseFormat = defaults.ResponseFormat
	}
	if defaults.Headers != nil {
		result.Headers = make(map[string]string)
		for k, v := range defaults.Headers {
			result.Headers[k] = v
		}
	}
	if defaults.MaxSteps != nil {
		result.MaxSteps = defaults.MaxSteps
	}

	// Override with values from overrides
	if overrides.Prompt.Messages != nil {
		result.Prompt.Messages = overrides.Prompt.Messages
	}
	if overrides.MaxTokens != nil {
		result.MaxTokens = overrides.MaxTokens
	}
	if overrides.Temperature != nil {
		result.Temperature = overrides.Temperature
	}
	if overrides.TopP != nil {
		result.TopP = overrides.TopP
	}
	if overrides.TopK != nil {
		result.TopK = overrides.TopK
	}
	if overrides.PresencePenalty != nil {
		result.PresencePenalty = overrides.PresencePenalty
	}
	if overrides.FrequencyPenalty != nil {
		result.FrequencyPenalty = overrides.FrequencyPenalty
	}
	if overrides.StopSequences != nil {
		result.StopSequences = overrides.StopSequences
	}
	if overrides.Seed != nil {
		result.Seed = overrides.Seed
	}
	if overrides.Tools != nil {
		result.Tools = overrides.Tools
	}
	if overrides.ToolChoice.Type != "" {
		result.ToolChoice = overrides.ToolChoice
	}
	if overrides.ResponseFormat != nil {
		result.ResponseFormat = overrides.ResponseFormat
	}
	if overrides.Headers != nil {
		if result.Headers == nil {
			result.Headers = make(map[string]string)
		}
		for k, v := range overrides.Headers {
			result.Headers[k] = v
		}
	}
	if overrides.MaxSteps != nil {
		result.MaxSteps = overrides.MaxSteps
	}

	return result
}
