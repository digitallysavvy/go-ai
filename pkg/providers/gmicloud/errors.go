package gmicloud

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// gmicloudErrorPayload documents the GMI Cloud API error shape:
// {"error": {"message": string, "type": string|null, "param": any,
// "code": string|number|null, "details": string|null}}. Mirrors TS
// gmicloudErrorDataSchema (gmicloud-error.ts). GMI's edge nests the backend
// engine's diagnostic in error.details as a JSON-encoded string, which the
// default OpenAI-compatible error handling would otherwise drop.
type gmicloudErrorPayload struct {
	Error struct {
		Message string      `json:"message"`
		Type    string      `json:"type,omitempty"`
		Param   interface{} `json:"param,omitempty"`
		Code    interface{} `json:"code,omitempty"`
		Details string      `json:"details,omitempty"`
	} `json:"error"`
}

// parseGmicloudProviderError extracts a GMI Cloud error envelope from an HTTP
// error response and returns a ProviderError. When error.details contains a
// JSON-encoded inner error with its own message, that inner message is
// surfaced instead of the outer (generic) backend message, mirroring TS
// gmicloudErrorStructure.errorToMessage.
func parseGmicloudProviderError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return nil
	}

	var payload gmicloudErrorPayload
	if unmarshalErr := json.Unmarshal(statusErr.Body, &payload); unmarshalErr != nil || payload.Error.Message == "" {
		return nil
	}

	message := payload.Error.Message
	if inner := unwrapGmicloudDetailsMessage(payload.Error.Details); inner != "" {
		message = inner
	}

	code := gmicloudErrorCode(payload.Error.Code)
	if code == "" {
		code = payload.Error.Type
	}

	return providererrors.NewProviderError("gmicloud", statusErr.StatusCode, code, message, err)
}

// unwrapGmicloudDetailsMessage parses error.details (a JSON-encoded string
// carrying the upstream engine's own {"error":{"message":...}} envelope) and
// returns its message, or "" if details is empty, unparsable, or has no
// non-empty message.
func unwrapGmicloudDetailsMessage(details string) string {
	if strings.TrimSpace(details) == "" {
		return ""
	}
	var inner struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(details), &inner); err != nil {
		return ""
	}
	return strings.TrimSpace(inner.Error.Message)
}

// gmicloudErrorCode normalizes error.code, which may be a JSON string or
// number, to a string.
func gmicloudErrorCode(code interface{}) string {
	switch c := code.(type) {
	case string:
		return c
	case float64:
		return strconv.FormatFloat(c, 'f', -1, 64)
	default:
		return ""
	}
}
