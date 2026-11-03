package anthropic

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// transformErrorBody mirrors createAmazonBedrockAnthropicFetch's non-ok
// response handling (amazon-bedrock-anthropic-fetch.ts): Bedrock returns a
// flat {message, ...} (or non-JSON) error body, but anthropic.LanguageModel's
// error handler expects the Anthropic shape {type:"error", error:{type,
// message}}. The body is read as response.text() (BOM dropped, invalid UTF-8
// replaced) and the new body is the JSON.stringify text (key order, no HTML
// escaping). Registered as anthropic.Config.TransformErrorBody.
func transformErrorBody(body []byte) []byte {
	text := textdecode.UTF8(body)
	var parsed struct {
		Message string `json:"message"`
	}
	message := text
	if err := json.Unmarshal([]byte(text), &parsed); err == nil && parsed.Message != "" {
		message = parsed.Message
	}
	encoded, err := jsonstr.Marshal(message)
	if err != nil {
		return body
	}
	return []byte(`{"type":"error","error":{"type":"error","message":` + string(encoded) + `}}`)
}
