package bedrock

import (
	"encoding/json"
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
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return !math.IsNaN(float64(x)) && !math.IsInf(float64(x), 0)
	case float64:
		// zod v4 z.number() rejects NaN and the infinities.
		return !math.IsNaN(x) && !math.IsInf(x, 0)
	case json.Number:
		f, err := x.Float64()
		return err == nil && !math.IsInf(f, 0)
	}
	return false
}

// firstAmazonBedrockOptions returns the key and value TS parseProviderOptions
// reads: `amazonBedrock` first, then the legacy `bedrock` key.
func firstAmazonBedrockOptions(providerOptions map[string]interface{}) (string, interface{}) {
	for _, key := range []string{"amazonBedrock", "bedrock"} {
		if providerOptions[key] != nil {
			return key, providerOptions[key]
		}
	}
	return "", nil
}

// validateEmbeddingProviderOptions mirrors the zod validation TS runs on the
// embedding providerOptions (amazonBedrockEmbeddingModelOptionsSchema): a
// wrong type or an out-of-set value fails with an InvalidArgumentError.
func validateEmbeddingProviderOptions(providerOptions map[string]interface{}) error {
	key, v := firstAmazonBedrockOptions(providerOptions)
	if key == "" {
		return nil
	}
	if err := validateAmazonBedrockEmbeddingOptions(v); err != nil {
		return invalidProviderOptions(key, err)
	}
	return nil
}

func validateAmazonBedrockEmbeddingOptions(v interface{}) error {
	opts, ok := v.(map[string]interface{})
	if !ok {
		return fmt.Errorf("expected object, received %T", v)
	}
	if err := checkNumberLiteral(opts, "dimensions", 1024, 512, 256); err != nil {
		return err
	}
	if err := checkBool(opts, "normalize"); err != nil {
		return err
	}
	if err := checkNumberLiteral(opts, "embeddingDimension", 256, 384, 1024, 3072); err != nil {
		return err
	}
	if err := checkEnum(opts, "embeddingPurpose", "GENERIC_INDEX", "TEXT_RETRIEVAL", "IMAGE_RETRIEVAL", "VIDEO_RETRIEVAL", "DOCUMENT_RETRIEVAL", "AUDIO_RETRIEVAL", "GENERIC_RETRIEVAL", "CLASSIFICATION", "CLUSTERING"); err != nil {
		return err
	}
	if err := checkEnum(opts, "inputType", "search_document", "search_query", "classification", "clustering"); err != nil {
		return err
	}
	if err := checkEnum(opts, "truncate", "NONE", "START", "END"); err != nil {
		return err
	}
	return checkNumberLiteral(opts, "outputDimension", 256, 512, 1024, 1536)
}

// validateImageProviderOptions mirrors the zod validation TS runs on the image
// providerOptions (amazonBedrockImageModelOptionsSchema).
func validateImageProviderOptions(providerOptions map[string]interface{}) error {
	key, v := firstAmazonBedrockOptions(providerOptions)
	if key == "" {
		return nil
	}
	opts, ok := v.(map[string]interface{})
	if !ok {
		return invalidProviderOptions(key, fmt.Errorf("expected object, received %T", v))
	}
	checks := []error{
		checkEnum(opts, "quality", "standard", "premium"),
		checkString(opts, "negativeText"),
		checkNumber(opts, "cfgScale"),
		checkEnum(opts, "style", "3D_ANIMATED_FAMILY_FILM", "DESIGN_SKETCH", "FLAT_VECTOR_ILLUSTRATION", "GRAPHIC_NOVEL_ILLUSTRATION", "MAXIMALISM", "MIDCENTURY_RETRO", "PHOTOREALISM", "SOFT_DIGITAL_PAINTING"),
		checkEnum(opts, "taskType", "TEXT_IMAGE", "IMAGE_VARIATION", "INPAINTING", "OUTPAINTING", "BACKGROUND_REMOVAL"),
		checkString(opts, "maskPrompt"),
		checkEnum(opts, "outPaintingMode", "DEFAULT", "PRECISE"),
		checkNumber(opts, "similarityStrength"),
	}
	for _, err := range checks {
		if err != nil {
			return invalidProviderOptions(key, err)
		}
	}
	return nil
}

// validateRerankProviderOptions mirrors the zod validation TS runs on the
// reranking providerOptions (amazonBedrockRerankingModelOptionsSchema):
// nextToken must be a string and additionalModelRequestFields an object.
func validateRerankProviderOptions(providerOptions map[string]interface{}) error {
	key, v := firstAmazonBedrockOptions(providerOptions)
	if key == "" {
		return nil
	}
	opts, ok := v.(map[string]interface{})
	if !ok {
		return invalidProviderOptions(key, fmt.Errorf("expected object, received %T", v))
	}
	if err := checkString(opts, "nextToken"); err != nil {
		return invalidProviderOptions(key, err)
	}
	if f, ok := opts["additionalModelRequestFields"]; ok && f != nil {
		if _, isMap := f.(map[string]interface{}); !isMap {
			return invalidProviderOptions(key, fmt.Errorf("additionalModelRequestFields: expected object, received %T", f))
		}
	}
	return nil
}

func checkString(m map[string]interface{}, key string) error {
	if v, ok := m[key]; ok && v != nil {
		if _, isStr := v.(string); !isStr {
			return fmt.Errorf("%s: expected string, received %T", key, v)
		}
	}
	return nil
}

func checkBool(m map[string]interface{}, key string) error {
	if v, ok := m[key]; ok && v != nil {
		if _, isBool := v.(bool); !isBool {
			return fmt.Errorf("%s: expected boolean, received %T", key, v)
		}
	}
	return nil
}

func checkNumber(m map[string]interface{}, key string) error {
	if v, ok := m[key]; ok && v != nil && !isNumber(v) {
		return fmt.Errorf("%s: expected number, received %T", key, v)
	}
	return nil
}

func checkNumberLiteral(m map[string]interface{}, key string, allowed ...float64) error {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	if isNumber(v) {
		if f, ok := toFloat64(v); ok {
			for _, a := range allowed {
				if f == a {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("%s: invalid option %v, expected one of %v", key, v, allowed)
}
