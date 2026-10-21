package moonshot

import (
	"encoding/json"

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
	// HasPromptCacheKey / HasSafetyIdentifier record that the option key was
	// present, so an empty string is still forwarded (TS `!= null`).
	HasPromptCacheKey   bool
	HasSafetyIdentifier bool
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

	// zod `.optional()` accepts an absent member but rejects a wrongly typed
	// one, an explicit null included.
	for _, key := range []string{"strictJsonSchema", "logprobs"} {
		if v, ok := opts[key]; ok {
			if _, isBool := v.(bool); !isBool {
				return result, invalidMoonshotProviderOptions(nil)
			}
		}
	}
	for _, key := range []string{"reasoningEffort", "reasoningHistory", "promptCacheKey", "safetyIdentifier"} {
		if v, ok := opts[key]; ok {
			if _, isString := v.(string); !isString {
				return result, invalidMoonshotProviderOptions(nil)
			}
		}
	}
	if v, ok := opts["thinking"]; ok {
		thinking, isObject := v.(map[string]interface{})
		if !isObject {
			return result, invalidMoonshotProviderOptions(nil)
		}
		if t, ok := thinking["type"]; ok {
			if _, isString := t.(string); !isString {
				return result, invalidMoonshotProviderOptions(nil)
			}
		}
		// budgetTokens: z.number().int().min(1024).
		if bt, ok := thinking["budgetTokens"]; ok {
			f, isNumber := moonshotNumber(bt)
			if !isNumber || f != float64(int64(f)) || f < 1024 {
				return result, invalidMoonshotProviderOptions(nil)
			}
		}
	}

	if v, ok := providerutils.OpenAICompatibleBoolOption(opts, "strictJsonSchema"); ok {
		result.HasStrictJSONSchema = true
		result.StrictJSONSchema = v
	}
	if v, ok := providerutils.OpenAICompatibleBoolOption(opts, "logprobs"); ok {
		result.HasLogprobs = true
		result.Logprobs = v
	}
	if v, ok := opts["topLogprobs"]; ok {
		if v == nil {
			return result, invalidMoonshotProviderOptions(nil)
		}
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
	if v, ok := opts["prediction"]; ok {
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
		if _, ok := v["budgetTokens"]; ok {
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
		result.HasPromptCacheKey = true
	}
	if v, ok := providerutils.OpenAICompatibleStringOption(opts, "safetyIdentifier"); ok {
		result.SafetyIdentifier = v
		result.HasSafetyIdentifier = true
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

	// HasTools records that the `tools` key was present (TS `tools != null`),
	// even when the list is empty.
	HasTools bool
	Tools    []types.Tool
}

// parseMoonshotMessageOptions parses+validates a message's provider options
// like TS parseProviderOptions with moonshotaiAllMessageProviderOptions: a
// non-string `name`, a `partial` other than true, an explicit null for any
// modeled key, or a malformed dynamic tool is an InvalidArgumentError.
func parseMoonshotMessageOptions(providerOptions map[string]interface{}) (*moonshotMessageOptions, error) {
	resolved, _ := providerutils.ResolveOpenAICompatibleProviderOptions("moonshot", providerOptions)
	mergeMoonshotAIProviderOptions(resolved, providerOptions)
	if len(resolved) == 0 {
		return nil, nil
	}

	result := &moonshotMessageOptions{}
	if v, ok := resolved["name"]; ok {
		name, isString := v.(string)
		if !isString {
			return nil, invalidMoonshotProviderOptions(nil)
		}
		result.HasName = true
		result.Name = name
	}
	if v, ok := resolved["partial"]; ok {
		if partial, isBool := v.(bool); !isBool || !partial {
			return nil, invalidMoonshotProviderOptions(nil)
		}
		result.Partial = true
	}
	if v, ok := resolved["tools"]; ok {
		toolsRaw, isList := v.([]interface{})
		if !isList {
			return nil, invalidMoonshotProviderOptions(nil)
		}
		result.HasTools = true
		tools := make([]types.Tool, 0, len(toolsRaw))
		for _, raw := range toolsRaw {
			toolMap, ok := raw.(map[string]interface{})
			if !ok {
				return nil, invalidMoonshotProviderOptions(nil)
			}
			// moonshotaiDynamicToolSchema.
			if t, _ := toolMap["type"].(string); t != "function" {
				return nil, invalidMoonshotProviderOptions(nil)
			}
			name, ok := toolMap["name"].(string)
			if !ok {
				return nil, invalidMoonshotProviderOptions(nil)
			}
			tool := types.Tool{Name: name}
			if d, ok := toolMap["description"]; ok {
				desc, isString := d.(string)
				if !isString {
					return nil, invalidMoonshotProviderOptions(nil)
				}
				tool.Description = desc
			}
			// zod z.record: the schema must be a JSON object. v0.5.2 accepted
			// any Go value here (json.RawMessage, a typed schema struct), so
			// any value that encodes to a JSON object is kept as given.
			params, present := toolMap["inputSchema"]
			if !present || !encodesToJSONObject(params) {
				return nil, invalidMoonshotProviderOptions(nil)
			}
			tool.Parameters = params
			if st, ok := toolMap["strict"]; ok {
				strict, isBool := st.(bool)
				if !isBool {
					return nil, invalidMoonshotProviderOptions(nil)
				}
				tool.Strict = &strict
			}
			tools = append(tools, tool)
		}
		result.Tools = tools
	}

	return result, nil
}

// moonshotNumber reads a numeric option given as any Go numeric type.
func moonshotNumber(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// encodesToJSONObject reports whether v is (or JSON-encodes to) a JSON object.
func encodesToJSONObject(v interface{}) bool {
	switch v.(type) {
	case nil:
		return false
	case map[string]interface{}:
		return true
	}
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	var obj map[string]json.RawMessage
	return json.Unmarshal(b, &obj) == nil && obj != nil
}
