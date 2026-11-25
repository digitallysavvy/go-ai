package liquid

import (
	"bytes"
	"encoding/json"
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// liquidHTTPError is liquidFailedResponseHandler (TS createJsonErrorResponseHandler
// with {error: {message: string, code?: string | null}}): a matching body
// gives "<message> (<code>)" (the code only when it is not null), anything
// else the response's status text.
func liquidHTTPError(err error) error {
	if cause, ok := internalhttp.FetchRejectionCause(err); ok {
		return cause
	}
	if noResp, ok := internalhttp.NoResponseError(err); ok {
		return noResp
	}
	if providererrors.IsLoadAPIKeyError(err) {
		return err
	}
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return err
	}
	message := statusErr.Text()
	var data interface{}
	if text, parsed, ok := liquidErrorMessage(statusErr.Body); ok {
		message, data = text, parsed
	}
	return &providererrors.ProviderError{
		Provider:        decisionProviderName,
		StatusCode:      statusErr.StatusCode,
		Message:         message,
		ResponseHeaders: lowerHeaders(providerutils.ExtractHeaders(statusErr.Headers)),
		ResponseBody:    string(statusErr.Body),
		Data:            data,
		Cause:           err,
	}
}

// liquidErrorMessage parses body against the error schema. It returns the
// message, zod's parsed value (only the schema's keys) and whether the body
// matched.
func liquidErrorMessage(body []byte) (string, interface{}, bool) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "", nil, false
	}
	var envelope map[string]json.RawMessage
	if securejson.Unmarshal(body, &envelope) != nil {
		return "", nil, false
	}
	raw, ok := envelope["error"]
	if !ok {
		return "", nil, false
	}
	var inner map[string]json.RawMessage
	if t := bytes.TrimSpace(raw); len(t) == 0 || t[0] != '{' || json.Unmarshal(raw, &inner) != nil {
		return "", nil, false
	}
	var message string
	if rawMessage, ok := inner["message"]; !ok || json.Unmarshal(rawMessage, &message) != nil || isJSONNull(rawMessage) {
		return "", nil, false
	}
	parsedError := map[string]interface{}{"message": message}
	text := message
	if rawCode, present := inner["code"]; present {
		if isJSONNull(rawCode) {
			parsedError["code"] = nil
		} else {
			var code string
			if json.Unmarshal(rawCode, &code) != nil {
				return "", nil, false
			}
			parsedError["code"] = code
			text += " (" + code + ")"
		}
	}
	return text, map[string]interface{}{"error": parsedError}, true
}

func isJSONNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }
