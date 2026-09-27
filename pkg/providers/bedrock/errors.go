package bedrock

import (
	"encoding/json"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// bedrockErrorBody mirrors the TS SDK's AmazonBedrockErrorSchema:
// { message: string, type?: string }.
type bedrockErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
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
	message := string(body)
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Message != "" {
		if parsed.Type != "" {
			message = parsed.Type + ": " + parsed.Message
		} else {
			message = parsed.Message
		}
	}
	providerErr := providererrors.NewProviderError("amazon-bedrock", statusCode, "", message, nil)
	providerErr.ResponseHeaders = headers
	providerErr.ResponseBody = string(body)
	return providerErr
}
