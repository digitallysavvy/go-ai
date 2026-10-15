package bedrock

import (
	"fmt"
	"math"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// validateChatProviderOptions mirrors the zod validation TS parseProviderOptions
// runs on providerOptions.amazonBedrock / .bedrock
// (amazonBedrockLanguageModelChatOptions) and providerOptions.anthropic
// (anthropicProviderOptions): a value of the wrong type or outside an enum
// fails the call with an InvalidArgumentError instead of being forwarded.
// Unknown keys are accepted (TS forwards them into the Converse body).
func validateChatProviderOptions(providerOptions map[string]interface{}) error {
	var amazonKey string
	for _, key := range []string{"amazonBedrock", "bedrock"} {
		if providerOptions[key] != nil {
			amazonKey = key
			break
		}
	}
	if amazonKey != "" {
		if err := validateAmazonBedrockOptions(providerOptions[amazonKey]); err != nil {
			return invalidProviderOptions(amazonKey, err)
		}
	}
	if providerOptions["anthropic"] != nil {
		if err := validateBedrockAnthropicOptions(providerOptions["anthropic"]); err != nil {
			return invalidProviderOptions("anthropic", err)
		}
	}
	return nil
}

func invalidProviderOptions(provider string, cause error) error {
	return &providererrors.InvalidArgumentError{
		Field:   "providerOptions",
		Message: fmt.Sprintf("invalid %s provider options", provider),
		Cause:   cause,
	}
}

func validateAmazonBedrockOptions(v interface{}) error {
	opts, ok := v.(map[string]interface{})
	if !ok {
		return fmt.Errorf("expected object, received %T", v)
	}
	if err := checkEnum(opts, "structuredOutputMode", "outputFormat", "jsonTool", "auto"); err != nil {
		return err
	}
	if err := checkEnum(opts, "serviceTier", "reserved", "priority", "default", "flex"); err != nil {
		return err
	}
	if x, ok := opts["additionalModelRequestFields"]; ok && x != nil {
		if _, isMap := x.(map[string]interface{}); !isMap {
			return fmt.Errorf("additionalModelRequestFields: expected object, received %T", x)
		}
	}
	if x, ok := opts["anthropicBeta"]; ok && x != nil {
		if err := checkStringArray("anthropicBeta", x); err != nil {
			return err
		}
	}
	if x, ok := opts["requestMetadata"]; ok && x != nil {
		m, isMap := x.(map[string]interface{})
		if !isMap {
			return fmt.Errorf("requestMetadata: expected object, received %T", x)
		}
		for k, val := range m {
			if _, isStr := val.(string); !isStr {
				return fmt.Errorf("requestMetadata.%s: expected string, received %T", k, val)
			}
		}
	}
	if x, ok := opts["reasoningConfig"]; ok && x != nil {
		rc, isMap := x.(map[string]interface{})
		if !isMap {
			return fmt.Errorf("reasoningConfig: expected object, received %T", x)
		}
		if err := checkEnum(rc, "type", "enabled", "disabled", "adaptive"); err != nil {
			return fmt.Errorf("reasoningConfig.%w", err)
		}
		if err := checkEnum(rc, "maxReasoningEffort", "low", "medium", "high", "xhigh", "max"); err != nil {
			return fmt.Errorf("reasoningConfig.%w", err)
		}
		if err := checkEnum(rc, "display", "omitted", "summarized"); err != nil {
			return fmt.Errorf("reasoningConfig.%w", err)
		}
		if b, ok := rc["budgetTokens"]; ok && b != nil {
			if !isNumber(b) {
				return fmt.Errorf("reasoningConfig.budgetTokens: expected number, received %T", b)
			}
		}
	}
	return nil
}

func validateBedrockAnthropicOptions(v interface{}) error {
	opts, ok := v.(map[string]interface{})
	if !ok {
		return fmt.Errorf("expected object, received %T", v)
	}
	if x, ok := opts["disableParallelToolUse"]; ok && x != nil {
		if _, isBool := x.(bool); !isBool {
			return fmt.Errorf("disableParallelToolUse: expected boolean, received %T", x)
		}
	}
	return checkEnum(opts, "structuredOutputMode", "outputFormat", "jsonTool", "auto")
}

func checkEnum(m map[string]interface{}, key string, allowed ...string) error {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	s, isStr := v.(string)
	if isStr {
		for _, a := range allowed {
			if s == a {
				return nil
			}
		}
	}
	return fmt.Errorf("%s: invalid option %v, expected one of %v", key, v, allowed)
}

func checkStringArray(key string, v interface{}) error {
	switch arr := v.(type) {
	case []string:
		return nil
	case []interface{}:
		for i, e := range arr {
			if _, ok := e.(string); !ok {
				return fmt.Errorf("%s[%d]: expected string, received %T", key, i, e)
			}
		}
		return nil
	}
	return fmt.Errorf("%s: expected array, received %T", key, v)
}

func isNumber(v interface{}) bool {
	switch x := v.(type) {
	case int, int32, int64, uint, uint32, uint64, float32:
		return true
	case float64:
		return !math.IsNaN(x)
	}
	return false
}
