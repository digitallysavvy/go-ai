package xai

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
