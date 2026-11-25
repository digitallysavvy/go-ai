package scaledown

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// scaledownHTTPError is scaledownFailedResponseHandler (TS
// createJsonErrorResponseHandler with {detail: string | {msg: string}[]}): a
// matching body gives the detail string, or the messages joined with ", "; any
// other body gives the response's status text.
func scaledownHTTPError(err error) error {
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
	if text, parsed, ok := scaledownErrorMessage(statusErr.Body); ok {
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

// scaledownErrorMessage parses body against the error schema, returning the
// message, zod's parsed value and whether the body matched.
func scaledownErrorMessage(body []byte) (string, interface{}, bool) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "", nil, false
	}
	var envelope map[string]json.RawMessage
	if securejson.Unmarshal(body, &envelope) != nil {
		return "", nil, false
	}
	raw, ok := envelope["detail"]
	if !ok {
		return "", nil, false
	}
	var detail string
	if json.Unmarshal(raw, &detail) == nil && !isJSONNull(raw) {
		return detail, map[string]interface{}{"detail": detail}, true
	}
	var items []map[string]json.RawMessage
	if t := bytes.TrimSpace(raw); len(t) == 0 || t[0] != '[' || json.Unmarshal(raw, &items) != nil {
		return "", nil, false
	}
	messages := make([]string, 0, len(items))
	parsed := make([]interface{}, 0, len(items))
	for _, item := range items {
		rawMsg, ok := item["msg"]
		var msg string
		if !ok || isJSONNull(rawMsg) || json.Unmarshal(rawMsg, &msg) != nil {
			return "", nil, false
		}
		messages = append(messages, msg)
		parsed = append(parsed, map[string]interface{}{"msg": msg})
	}
	return strings.Join(messages, ", "), map[string]interface{}{"detail": parsed}, true
}

func isJSONNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }
