package moonshot

import (
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// mergeMoonshotAIProviderOptions merges providerOptions.moonshotai into an
// already-resolved options map (in place), giving it precedence. TS's
// provider config is "moonshotai.chat" (moonshotai-provider.ts), so its
// providerOptionsName (config.provider.split('.')[0]) is "moonshotai" — not
// "moonshot", which is this Go SDK's own package/provider-name convention
// (mirrors the same fix in the together package, whose TS provider name is
// "togetherai"). Without this, a caller following TS docs/examples and
// writing providerOptions.moonshotai.* is silently ignored.
func mergeMoonshotAIProviderOptions(resolved, providerOptions map[string]interface{}) {
	moonshotAI, ok := providerOptions["moonshotai"].(map[string]interface{})
	if !ok {
		return
	}
	for k, v := range moonshotAI {
		resolved[k] = v
	}
}

// moonshotThinkingOption mirrors the parsed providerOptions.moonshot.thinking
// object (TS: { type?: 'enabled'|'disabled', budgetTokens?: number }).
type moonshotThinkingOption struct {
	Type            string
	HasBudgetTokens bool
}

// moonshotModelOptions mirrors the parsed moonshotaiLanguageModelOptions
// schema (TS moonshotai-chat-options.ts).
type moonshotModelOptions struct {
	HasStrictJSONSchema bool
	StrictJSONSchema    bool

	HasLogprobs bool
	Logprobs    bool

	HasTopLogprobs bool
	TopLogprobs    int

	ReasoningEffort string // "low" | "high" | "max" | ""

	HasPrediction bool
	Prediction    interface{} // validated, wire-ready {"type":"content","content":...}

	Thinking *moonshotThinkingOption

	ReasoningHistory string // "disabled" | "interleaved" | "preserved" | ""

	PromptCacheKey   string
	SafetyIdentifier string
}

func invalidMoonshotProviderOptions(cause error) error {
	return &providererrors.InvalidArgumentError{
		Field:   "providerOptions",
		Message: "invalid moonshotai provider options",
		Cause:   cause,
	}
}

// parseMoonshotModelOptions parses+validates the resolved moonshot provider
// options map into a typed struct, mirroring TS parseProviderOptions with the
// moonshotaiLanguageModelOptions zod schema.
func parseMoonshotModelOptions(opts map[string]interface{}) (moonshotModelOptions, error) {
	var result moonshotModelOptions

	if v, ok := providerutils.OpenAICompatibleBoolOption(opts, "strictJsonSchema"); ok {
		result.HasStrictJSONSchema = true
		result.StrictJSONSchema = v
	}
	if v, ok := providerutils.OpenAICompatibleBoolOption(opts, "logprobs"); ok {
		result.HasLogprobs = true
		result.Logprobs = v
	}
	if v, ok := opts["topLogprobs"]; ok && v != nil {
		if f, isFloat := v.(float64); isFloat && f != float64(int(f)) {
			return result, invalidMoonshotProviderOptions(nil)
		}
		n, ok := providerutils.OpenAICompatibleIntOption(opts, "topLogprobs")
		if !ok || n < 0 || n > 20 {
			return result, invalidMoonshotProviderOptions(nil)
		}
		result.HasTopLogprobs = true
		result.TopLogprobs = n
	}
	if v, ok := providerutils.OpenAICompatibleStringOption(opts, "reasoningEffort"); ok {
		if v != "low" && v != "high" && v != "max" {
			return result, invalidMoonshotProviderOptions(nil)
		}
		result.ReasoningEffort = v
	}
	if v, ok := opts["prediction"]; ok && v != nil {
		prediction, err := validateMoonshotPrediction(v)
		if err != nil {
			return result, err
		}
		result.HasPrediction = true
		result.Prediction = prediction
	}
	if v, ok := opts["thinking"].(map[string]interface{}); ok {
		thinking := &moonshotThinkingOption{}
		if t, ok := providerutils.OpenAICompatibleStringOption(v, "type"); ok {
			if t != "enabled" && t != "disabled" {
				return result, invalidMoonshotProviderOptions(nil)
			}
			thinking.Type = t
		}
		if bt, ok := v["budgetTokens"]; ok && bt != nil {
			thinking.HasBudgetTokens = true
		}
		result.Thinking = thinking
	}
	if v, ok := providerutils.OpenAICompatibleStringOption(opts, "reasoningHistory"); ok {
		if v != "disabled" && v != "interleaved" && v != "preserved" {
			return result, invalidMoonshotProviderOptions(nil)
		}
		result.ReasoningHistory = v
	}
	if v, ok := providerutils.OpenAICompatibleStringOption(opts, "promptCacheKey"); ok {
		result.PromptCacheKey = v
	}
	if v, ok := providerutils.OpenAICompatibleStringOption(opts, "safetyIdentifier"); ok {
		result.SafetyIdentifier = v
	}

	return result, nil
}

// validateMoonshotPrediction validates providerOptions.moonshot.prediction
// against the TS schema: { type: 'content', content: string | Array<{type:
// 'text', text: string}> }. Returns the wire-ready value.
func validateMoonshotPrediction(value interface{}) (interface{}, error) {
	m, ok := value.(map[string]interface{})
	if !ok {
		return nil, invalidMoonshotProviderOptions(nil)
	}
	if t, _ := m["type"].(string); t != "content" {
		return nil, invalidMoonshotProviderOptions(nil)
	}
	content, ok := m["content"]
	if !ok {
		return nil, invalidMoonshotProviderOptions(nil)
	}
	switch c := content.(type) {
	case string:
		return map[string]interface{}{"type": "content", "content": c}, nil
	case []interface{}:
		parts := make([]map[string]interface{}, 0, len(c))
		for _, item := range c {
			part, ok := item.(map[string]interface{})
			if !ok {
				return nil, invalidMoonshotProviderOptions(nil)
			}
			if t, _ := part["type"].(string); t != "text" {
				return nil, invalidMoonshotProviderOptions(nil)
			}
			text, ok := part["text"].(string)
			if !ok {
				return nil, invalidMoonshotProviderOptions(nil)
			}
			parts = append(parts, map[string]interface{}{"type": "text", "text": text})
		}
		return map[string]interface{}{"type": "content", "content": parts}, nil
	default:
		return nil, invalidMoonshotProviderOptions(nil)
	}
}

// moonshotMessageOptions mirrors moonshotaiAllMessageProviderOptions: message
// name/partial-mode/dynamic-tools passthrough parsed from a single message's
// ProviderOptions["moonshot"] map.
type moonshotMessageOptions struct {
	HasName bool
	Name    string

	Partial bool

	Tools []types.Tool
}

func parseMoonshotMessageOptions(providerOptions map[string]interface{}) (*moonshotMessageOptions, error) {
	resolved, _ := providerutils.ResolveOpenAICompatibleProviderOptions("moonshot", providerOptions)
	mergeMoonshotAIProviderOptions(resolved, providerOptions)
	if len(resolved) == 0 {
		return nil, nil
	}

	result := &moonshotMessageOptions{}
	if name, ok := providerutils.OpenAICompatibleStringOption(resolved, "name"); ok {
		result.HasName = true
		result.Name = name
	}
	if partial, ok := providerutils.OpenAICompatibleBoolOption(resolved, "partial"); ok {
		result.Partial = partial
	}
	if toolsRaw, ok := resolved["tools"].([]interface{}); ok {
		tools := make([]types.Tool, 0, len(toolsRaw))
		for _, raw := range toolsRaw {
			toolMap, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			tool := types.Tool{}
			if name, ok := toolMap["name"].(string); ok {
				tool.Name = name
			}
			if desc, ok := toolMap["description"].(string); ok {
				tool.Description = desc
			}
			if params, ok := toolMap["inputSchema"]; ok {
				tool.Parameters = params
			}
			if strict, ok := toolMap["strict"].(bool); ok {
				tool.Strict = &strict
			}
			tools = append(tools, tool)
		}
		result.Tools = tools
	}

	return result, nil
}
