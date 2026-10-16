package bedrock

import (
	"encoding/json"
	"net/http"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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
	var parsed bedrockErrorBody
	// TS createJsonErrorResponseHandler: an empty or unparseable body gives the
	// response status text (the body stays on ResponseBody).
	message := http.StatusText(statusCode)
	if message == "" {
		message = string(body)
	}
	// TS: error.type == null ? error.message : `${error.type}: ${error.message}`;
	// an empty string is a valid message or type.
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Message != nil {
		if parsed.Type != nil {
			message = *parsed.Type + ": " + *parsed.Message
		} else {
			message = *parsed.Message
		}
	}
	providerErr := providererrors.NewProviderError("amazon-bedrock", statusCode, "", message, nil)
	providerErr.ResponseHeaders = headers
	providerErr.ResponseBody = string(body)
	return providerErr
}

// bedrockConverseGenerateAPIError is the Converse (non-streaming) variant:
// TS amazon-bedrock-chat-language-model.ts#doGenerate builds its own handler
// with `errorToMessage: error => error.message ?? 'Unknown error'`, so the
// error "type" is not part of the message there (unlike every other Bedrock
// endpoint, which uses the shared handler above).
func bedrockConverseGenerateAPIError(statusCode int, body []byte, headers map[string]string) *providererrors.ProviderError {
	var parsed bedrockErrorBody
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Message == nil {
		return bedrockAPIError(statusCode, body, headers)
	}
	providerErr := providererrors.NewProviderError("amazon-bedrock", statusCode, "", *parsed.Message, nil)
	providerErr.ResponseHeaders = headers
	providerErr.ResponseBody = string(body)
	return providerErr
}
