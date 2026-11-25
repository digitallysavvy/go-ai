package anthropic

import (
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
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
	// TS safeParseJSON (secureJsonParse) against z.looseObject({message:
	// z.string().optional()}): a truthy string message, else the text.
	message := text
	if v, ok := prompt.SecureJSONParse(text); ok {
		if obj, ok := v.(map[string]interface{}); ok {
			if m, ok := obj["message"].(string); ok && m != "" {
				message = m
			}
		}
	}
	encoded, err := jsonstr.Marshal(message)
	if err != nil {
		return body
	}
	return []byte(`{"type":"error","error":{"type":"error","message":` + string(encoded) + `}}`)
}
