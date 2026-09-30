package fal

import "encoding/json"

// parseFalErrorMessage extracts the message from a fal.ai error response
// body shaped like {"error":{"message":...,"code":...}}, matching the TS
// SDK's falErrorDataSchema / falFailedResponseHandler
// (fal-error.ts). Falls back to fallback when the body doesn't match the
// expected shape.
func parseFalErrorMessage(body []byte, fallback string) string {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	return fallback
}
