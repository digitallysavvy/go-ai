package bedrock

import (
	"encoding/json"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// validateConverseUsage ports the TS zod usage schema of the Converse
// response and stream metadata (AmazonBedrockResponseSchema.usage):
// inputTokens, outputTokens and totalTokens must be numbers,
// cacheReadInputTokens and cacheWriteInputTokens a number or null, and
// cacheDetails a list of {inputTokens: number, ttl: string} objects. It
// returns nil for a valid or absent usage.
func validateConverseUsage(raw json.RawMessage) *providererrors.ValidationError {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var usage map[string]json.RawMessage
	if err := json.Unmarshal(raw, &usage); err != nil {
		return usageValidationError("usage", "expected object")
	}
	isNumber := func(v json.RawMessage) bool {
		var n float64
		return json.Unmarshal(v, &n) == nil && len(v) > 0 && v[0] != '"'
	}
	for _, field := range []string{"inputTokens", "outputTokens", "totalTokens"} {
		if v, ok := usage[field]; !ok || !isNumber(v) {
			return usageValidationError(field, "expected number")
		}
	}
	for _, field := range []string{"cacheReadInputTokens", "cacheWriteInputTokens"} {
		if v, ok := usage[field]; ok && string(v) != "null" && !isNumber(v) {
			return usageValidationError(field, "expected number")
		}
	}
	if v, ok := usage["cacheDetails"]; ok && string(v) != "null" {
		var details []map[string]json.RawMessage
		if err := json.Unmarshal(v, &details); err != nil {
			return usageValidationError("cacheDetails", "expected array")
		}
		for _, d := range details {
			if n, ok := d["inputTokens"]; !ok || !isNumber(n) {
				return usageValidationError("inputTokens", "expected number")
			}
			var ttl string
			if t, ok := d["ttl"]; !ok || json.Unmarshal(t, &ttl) != nil {
				return usageValidationError("ttl", "expected string")
			}
		}
	}
	return nil
}

func usageValidationError(field, problem string) *providererrors.ValidationError {
	return &providererrors.ValidationError{
		Field:   field,
		Message: fmt.Sprintf(`Type validation failed: invalid usage at "%s": %s`, field, problem),
	}
}

// invalidConverseResponseError is the APICallError TS throws when the
// response body does not match the schema.
func invalidConverseResponseError(cause error) error {
	return providererrors.NewProviderError("amazon-bedrock", 0, "", "Invalid JSON response", cause)
}
