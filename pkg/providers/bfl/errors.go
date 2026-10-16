package bfl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// bflStatusError builds the error for a non-2xx Black Forest Labs response,
// mirroring TS bflFailedResponseHandler (black-forest-labs-api.ts):
//   - the message is `detail` when it is a string, the compact JSON of any
//     other non-null `detail`, else `message`; failing that,
//     "Black Forest Labs generation failed: <status|state>." and finally
//     "Unknown Black Forest Labs error";
//   - a body that does not match the error schema (empty, not an object, a
//     wrongly typed field) gives the HTTP status text;
//   - a terminal task status (Error, Failed, moderation, Task not found) is
//     never retryable even on HTTP 503; otherwise 408, 409, 429 and 5xx are.
func bflStatusError(statusCode int, headers http.Header, body []byte) error {
	cause := &internalhttp.HTTPStatusError{StatusCode: statusCode, Headers: headers, Body: body}

	var raw struct {
		Message *string         `json:"message"`
		Detail  json.RawMessage `json:"detail"`
		Status  *string         `json:"status"`
		State   *string         `json:"state"`
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(body, &raw) != nil {
		return providererrors.NewProviderErrorFromResponse("bfl", 0, "", http.StatusText(statusCode), cause)
	}

	taskStatus := ""
	if raw.Status != nil {
		taskStatus = *raw.Status
	} else if raw.State != nil {
		taskStatus = *raw.State
	}

	var message string
	haveMessage := false
	detailPresent := len(raw.Detail) > 0 && string(bytes.TrimSpace(raw.Detail)) != "null"
	switch {
	case detailPresent:
		var s string
		if json.Unmarshal(raw.Detail, &s) == nil {
			message = s
		} else {
			var compact bytes.Buffer
			if json.Compact(&compact, raw.Detail) == nil {
				message = compact.String()
			}
		}
		haveMessage = true
	case raw.Message != nil:
		message, haveMessage = *raw.Message, true
	}
	if !haveMessage {
		if raw.Status != nil || raw.State != nil {
			message = fmt.Sprintf("Black Forest Labs generation failed: %s.", taskStatus)
		} else {
			message = "Unknown Black Forest Labs error"
		}
	}

	perr := providererrors.NewProviderErrorFromResponse("bfl", 0, "", message, cause)
	if bflTerminalTaskStatus(taskStatus) {
		notRetryable := false
		perr.Retryable = &notRetryable
	}
	return perr
}
