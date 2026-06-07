package policy

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/middleware"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// DefaultOPACapabilityInput is the default input passed to OPA capability rules.
type DefaultOPACapabilityInput struct {
	Messages        any `json:"messages"`
	ProviderOptions any `json:"providerOptions,omitempty"`
}

// CapabilityMiddlewareOptions configures OPACapabilityMiddleware.
type CapabilityMiddlewareOptions struct {
	Path    string
	ToInput func(params *provider.GenerateOptions) any
}

// OPACapabilityMiddleware narrows the model tool list to an OPA allowlist.
func OPACapabilityMiddleware(client PolicyClient, opts ...CapabilityMiddlewareOptions) *middleware.LanguageModelMiddleware {
	cfg := CapabilityMiddlewareOptions{}
	if len(opts) > 0 {
		cfg = opts[0]
	}
	return &middleware.LanguageModelMiddleware{
		SpecificationVersion: "v4",
		TransformParams: func(ctx context.Context, callType string, params *provider.GenerateOptions, model provider.LanguageModel) (*provider.GenerateOptions, error) {
			if params == nil || len(params.Tools) == 0 {
				return params, nil
			}
			if client == nil {
				cloned := *params
				cloned.Tools = nil
				return &cloned, nil
			}
			result, err := client.Evaluate(ctx, cfg.Path, capabilityInput(params, cfg.ToInput))
			if err != nil {
				cloned := *params
				cloned.Tools = nil
				return &cloned, nil
			}
			allowed := extractAllowedNameSet(result)
			if allowed == nil {
				cloned := *params
				cloned.Tools = nil
				return &cloned, nil
			}
			removed := false
			filtered := params.Tools[:0:0]
			for _, tool := range params.Tools {
				if isToolAllowed(tool, allowed) {
					filtered = append(filtered, tool)
					continue
				}
				removed = true
			}
			if !removed {
				return params, nil
			}
			cloned := *params
			cloned.Tools = filtered
			return &cloned, nil
		},
	}
}

func capabilityInput(params *provider.GenerateOptions, mapper func(*provider.GenerateOptions) any) any {
	if mapper != nil {
		if input := mapper(params); input != nil {
			return input
		}
	}
	return DefaultOPACapabilityInput{Messages: params.Prompt, ProviderOptions: params.ProviderOptions}
}

func extractAllowedNameSet(result any) map[string]bool {
	if record, ok := result.(map[string]any); ok {
		if tools, ok := record["tools"]; ok {
			result = tools
		}
	}
	if record, ok := result.(map[string][]string); ok {
		result = record["tools"]
	}
	switch list := result.(type) {
	case []any:
		out := make(map[string]bool, len(list))
		for _, item := range list {
			name, ok := item.(string)
			if !ok {
				return nil
			}
			out[name] = true
		}
		return out
	case []string:
		out := make(map[string]bool, len(list))
		for _, name := range list {
			out[name] = true
		}
		return out
	default:
		return nil
	}
}

func isToolAllowed(tool types.Tool, allowed map[string]bool) bool {
	for _, name := range toolPolicyNames(tool) {
		if allowed[name] {
			return true
		}
	}
	return false
}

func toolPolicyNames(tool types.Tool) []string {
	names := make([]string, 0, 2)
	if tool.Name != "" {
		names = append(names, tool.Name)
	}
	if tool.Type != types.ToolTypeProviderDefined {
		return names
	}
	if tool.ProviderID != "" {
		names = append(names, tool.ProviderID)
	}
	return names
}
