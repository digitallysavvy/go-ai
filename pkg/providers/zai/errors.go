package zai

import (
	"encoding/json"
	"errors"
	"strconv"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// zaiErrorPayload documents Z.AI's error envelope, a union of a bare
// {code, message} object and a {error: {code, message}} wrapper. Mirrors TS
// zaiErrorSchema (zai-error.ts):
//
//	z.union([{code, message}, {error: {code, message}}])
type zaiErrorPayload struct {
	Code    interface{} `json:"code,omitempty"`
	Message string      `json:"message,omitempty"`
	Error   *struct {
		Code    interface{} `json:"code,omitempty"`
		Message string      `json:"message"`
	} `json:"error,omitempty"`
}

// parseZaiProviderError extracts a Z.AI error envelope from an HTTP error
// response and returns a ProviderError carrying the provider's error code.
// Returns nil (letting the caller fall back to a generic error) when the
// body doesn't match the expected shape.
func parseZaiProviderError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return nil
	}

	var payload zaiErrorPayload
	if unmarshalErr := json.Unmarshal(statusErr.Body, &payload); unmarshalErr != nil {
		return nil
	}

	var message string
	var code interface{}
	switch {
	case payload.Error != nil && payload.Error.Message != "":
		message = payload.Error.Message
		code = payload.Error.Code
	case payload.Message != "":
		message = payload.Message
		code = payload.Code
	default:
		return nil
	}

	return providererrors.NewProviderError("zai", statusErr.StatusCode, zaiErrorCode(code), message, err)
}

// zaiErrorCode normalizes an error code, which may be a JSON string or
// number, to a string.
func zaiErrorCode(code interface{}) string {
	switch c := code.(type) {
	case string:
		return c
	case float64:
		return strconv.FormatFloat(c, 'f', -1, 64)
	default:
		return ""
	}
}
