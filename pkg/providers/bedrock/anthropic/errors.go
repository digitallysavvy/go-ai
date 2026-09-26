package anthropic

import "encoding/json"

// transformErrorBody mirrors createAmazonBedrockAnthropicFetch's non-ok
// response handling (amazon-bedrock-anthropic-fetch.ts, ai@7.0.113): Bedrock
// returns a flat {message, ...} (or non-JSON) error body, but
// anthropic.LanguageModel's error handler expects the Anthropic shape
// {type:"error", error:{type, message}}. Registered as
// anthropic.Config.TransformErrorBody.
func transformErrorBody(body []byte) []byte {
	var parsed struct {
		Message string `json:"message"`
	}
	message := string(body)
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Message != "" {
		message = parsed.Message
	}
	out, err := json.Marshal(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "error",
			"message": message,
		},
	})
	if err != nil {
		return body
	}
	return out
}
