package xai

import (
	"encoding/json"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func newXAIProviderError(provider string, statusCode int, body []byte) *providererrors.ProviderError {
	var responsesError struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &responsesError); err == nil && responsesError.Code != "" && responsesError.Error != "" {
		return providererrors.NewProviderError(provider, statusCode, responsesError.Code, responsesError.Code+": "+responsesError.Error, nil)
	}

	var parsed struct {
		Error struct {
			Message string      `json:"message"`
			Type    string      `json:"type"`
			Code    interface{} `json:"code"`
		} `json:"error"`
	}

	message := string(body)
	errorCode := ""
	if err := json.Unmarshal(body, &parsed); err == nil {
		if parsed.Error.Message != "" {
			message = parsed.Error.Message
		}
		switch code := parsed.Error.Code.(type) {
		case string:
			errorCode = code
		case float64:
			errorCode = fmt.Sprintf("%.0f", code)
		}
		if errorCode == "" {
			errorCode = parsed.Error.Type
		}
	}

	return providererrors.NewProviderError(provider, statusCode, errorCode, message, nil)
}

func formatXAIResponseError(code string, message string) string {
	if code != "" && message != "" {
		return code + ": " + message
	}
	return message
}

// ModerationError is returned by the xAI video model when the API rejects
// generated content due to content moderation policy.
type ModerationError struct {
	Code    string
	Message string
}

// Error implements the error interface.
func (e *ModerationError) Error() string {
	if e.Code != "" {
		return "xai: moderation rejection [" + e.Code + "]: " + e.Message
	}
	return "xai: moderation rejection: " + e.Message
}

// XAIStreamError represents a terminal stream "error" event from xAI Responses.
type XAIStreamError struct {
	Code    string
	Message string
}

func (e *XAIStreamError) Error() string {
	if e.Code != "" {
		return "xai.responses stream error [" + e.Code + "]: " + e.Message
	}
	return "xai.responses stream error: " + e.Message
}

// XAIStreamIncomplete represents a response.incomplete SSE event.
type XAIStreamIncomplete struct {
	Reason string
}

func (e *XAIStreamIncomplete) Error() string {
	if e.Reason != "" {
		return "xai.responses stream incomplete: " + e.Reason
	}
	return "xai.responses stream incomplete"
}

// XAIStreamFailed represents a response.failed SSE event.
type XAIStreamFailed struct {
	Reason  string
	Code    string
	Message string
}

func (e *XAIStreamFailed) Error() string {
	if e.Message != "" {
		return "xai.responses stream failed: " + e.Message
	}
	if e.Reason != "" {
		return "xai.responses stream failed: " + e.Reason
	}
	if e.Code != "" {
		return "xai.responses stream failed: " + e.Code
	}
	return "xai.responses stream failed"
}
