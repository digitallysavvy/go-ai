package gmicloud

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// gmicloudErrorPayload documents the GMI Cloud API error shape:
// {"error": {"message": string, "type": string|null, "param": any,
// "code": string|number|null, "details": string|null}}. Mirrors TS
// gmicloudErrorDataSchema (gmicloud-error.ts). GMI's edge nests the backend
// engine's diagnostic in error.details as a JSON-encoded string, which the
// default OpenAI-compatible error handling would otherwise drop.
type gmicloudErrorPayload struct {
	Error *struct {
		Message *string         `json:"message"`
		Type    *string         `json:"type"`
		Param   interface{}     `json:"param"`
		Code    json.RawMessage `json:"code"`
		Details *string         `json:"details"`
	} `json:"error"`
}

// parseGmicloudProviderError builds the ProviderError for an HTTP error
// response, or returns nil when err carries no HTTP response. A body matching
// gmicloudErrorDataSchema gives its message (even an empty one); when
// error.details contains a JSON-encoded inner error with its own message,
// that inner message is surfaced instead of the outer (generic) backend
// message, mirroring TS gmicloudErrorStructure.errorToMessage. A body that
// does not match the schema gives the HTTP status text.
func parseGmicloudProviderError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return nil
	}

	message := http.StatusText(statusErr.StatusCode)
	code := ""
	var payload gmicloudErrorPayload
	if unmarshalErr := json.Unmarshal(statusErr.Body, &payload); unmarshalErr == nil && payload.Error != nil &&
		payload.Error.Message != nil && gmicloudCodeMatchesSchema(payload.Error.Code) {
		message = *payload.Error.Message
		if payload.Error.Details != nil {
			if inner := unwrapGmicloudDetailsMessage(*payload.Error.Details); inner != "" {
				message = inner
			}
		}
		code = gmicloudErrorCode(payload.Error.Code)
		if code == "" && payload.Error.Type != nil {
			code = *payload.Error.Type
		}
	}

	return providererrors.NewProviderErrorFromResponse("gmicloud", 0, code, message, err)
}

// gmicloudCodeMatchesSchema reports whether raw is absent, null, a string or
// a number (TS z.union([z.string(), z.number()]).nullish()).
func gmicloudCodeMatchesSchema(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return true
	}
	var s string
	if json.Unmarshal(trimmed, &s) == nil {
		return true
	}
	var n float64
	return json.Unmarshal(trimmed, &n) == nil
}

// unwrapGmicloudDetailsMessage parses error.details (a JSON-encoded string
// carrying the upstream engine's own {"error":{"message":...}} envelope) and
// returns its message, or "" if details is empty, unparsable, or has no
// non-empty message.
func unwrapGmicloudDetailsMessage(details string) string {
	if details == "" {
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
	// TS checks message.trim() !== '' but returns the message untrimmed.
	if providerutils.JSTrim(inner.Error.Message) == "" {
		return ""
	}
	return inner.Error.Message
}

// gmicloudErrorCode normalizes error.code, which may be a JSON string or
// number, to a string.
func gmicloudErrorCode(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil && len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		return str
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	return ""
}
