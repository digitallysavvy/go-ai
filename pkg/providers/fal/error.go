package fal

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// parseFalErrorMessage extracts the message from a fal.ai error response
// body, matching the TS SDK's falErrorDataSchema / falFailedResponseHandler
// (fal-error.ts): {"error":{"message":string,"code":number}} gives its
// message, any other body gives fallback (callers pass the HTTP status text,
// which is what createJsonErrorResponseHandler uses).
func parseFalErrorMessage(body []byte, fallback string) string {
	message, code := providerutils.NestedErrorMessage(0, body)
	if code == "" && message == "" {
		return fallback
	}
	return message
}
