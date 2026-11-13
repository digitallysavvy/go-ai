package bedrock

import (
	"encoding/json"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// bedrockErrorBody mirrors the TS SDK's AmazonBedrockErrorSchema:
// { message: string, type?: string }.
type bedrockErrorBody struct {
	Message *string `json:"message"`
	Type    *string `json:"type"`
}

// bedrockAPIError parses a failed Bedrock HTTP response body and returns a
// *providererrors.ProviderError with a message matching the TS SDK's
// amazonBedrockFailedResponseHandler: "{type}: {message}" when a type is
// present, otherwise just "{message}". Falls back to the raw body text when
// the body cannot be parsed as JSON.
//
// headers and body are carried on the returned ProviderError's
// ResponseHeaders/ResponseBody fields, matching TS APICallError (which
// createJsonErrorResponseHandler always populates with responseHeaders and
// responseBody from the failed HTTP response).
func bedrockAPIError(statusCode int, body []byte, headers map[string]string) *providererrors.ProviderError {
	// TS createJsonErrorResponseHandler reads response.text() (BOM dropped,
	// invalid UTF-8 replaced).
	body = []byte(textdecode.UTF8(body))
	var parsed bedrockErrorBody
	// TS createJsonErrorResponseHandler: an empty or unparseable body gives the
	// response status text (the body stays on ResponseBody).
	message := http.StatusText(statusCode)
	if message == "" {
		message = string(body)
	}
	// TS: error.type == null ? error.message : `${error.type}: ${error.message}`;
	// an empty string is a valid message or type.
	var data interface{}
	if err := jsonexact.Unmarshal(body, &parsed); err == nil && parsed.Message != nil && secureJSON(body) {
		if parsed.Type != nil {
			message = *parsed.Type + ": " + *parsed.Message
		} else {
			message = *parsed.Message
		}
		data = bedrockErrorData(body, parsed)
	}
	providerErr := providererrors.NewProviderError("amazon-bedrock", statusCode, "", message, nil)
	providerErr.ResponseHeaders = headers
	providerErr.ResponseBody = string(body)
	providerErr.Data = data
	return providerErr
}

// bedrockErrorData is TS APICallError.data for a body that matched
// AmazonBedrockErrorSchema: the zod output {message, type?} (unknown keys
// stripped, a null type kept).
func bedrockErrorData(body []byte, parsed bedrockErrorBody) map[string]interface{} {
	data := map[string]interface{}{"message": *parsed.Message}
	var keys map[string]json.RawMessage
	if jsonexact.Unmarshal(body, &keys) == nil {
		if _, ok := keys["type"]; ok {
			if parsed.Type != nil {
				data["type"] = *parsed.Type
			} else {
				data["type"] = nil
			}
		}
	}
	return data
}

// bedrockConverseGenerateAPIError is the Converse (non-streaming) variant:
// TS amazon-bedrock-chat-language-model.ts#doGenerate builds its own handler
// with `errorToMessage: error => error.message ?? 'Unknown error'`, so the
// error "type" is not part of the message there (unlike every other Bedrock
// endpoint, which uses the shared handler above).
func bedrockConverseGenerateAPIError(statusCode int, body []byte, headers map[string]string) *providererrors.ProviderError {
	body = []byte(textdecode.UTF8(body))
	var parsed bedrockErrorBody
	if err := jsonexact.Unmarshal(body, &parsed); err != nil || parsed.Message == nil || !secureJSON(body) {
		return bedrockAPIError(statusCode, body, headers)
	}
	providerErr := providererrors.NewProviderError("amazon-bedrock", statusCode, "", *parsed.Message, nil)
	providerErr.ResponseHeaders = headers
	providerErr.ResponseBody = string(body)
	providerErr.Data = bedrockErrorData(body, parsed)
	return providerErr
}

// secureJSON reports a body TS secureJsonParse accepts (no __proto__ /
// constructor.prototype keys); TS safeParseJSON fails otherwise.
func secureJSON(body []byte) bool {
	_, ok := prompt.SecureJSONParse(string(body))
	return ok
}
