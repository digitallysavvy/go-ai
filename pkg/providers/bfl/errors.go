package bfl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
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
	body = []byte(textdecode.UTF8(body)) // TS reads the failed body as text (TextDecoder)
	cause := &internalhttp.HTTPStatusError{StatusCode: statusCode, Headers: headers, Body: body}

	var raw struct {
		Message json.RawMessage `json:"message"`
		Detail  json.RawMessage `json:"detail"`
		Status  *string         `json:"status"`
		State   *string         `json:"state"`
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' || securejson.Unmarshal(body, &raw) != nil {
		return bflOffSchemaError(statusCode, cause)
	}

	taskStatus := ""
	if raw.Status != nil {
		taskStatus = *raw.Status
	} else if raw.State != nil {
		taskStatus = *raw.State
	}

	// message is z.string().optional(): null or a non-string is off-schema.
	var messageText string
	if len(raw.Message) > 0 {
		if string(raw.Message) == "null" || json.Unmarshal(raw.Message, &messageText) != nil {
			return bflOffSchemaError(statusCode, cause)
		}
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
			// JSON.stringify(detail): numbers go through a double, strings
			// are re-escaped, duplicate keys collapse.
			if tree, ok := jsonorder.Parse(raw.Detail); ok {
				message = jsonorder.Stringify(tree)
			}
		}
		haveMessage = true
	case len(raw.Message) > 0:
		message, haveMessage = messageText, true
	}
	if !haveMessage {
		if raw.Status != nil || raw.State != nil {
			message = fmt.Sprintf("Black Forest Labs generation failed: %s.", taskStatus)
		} else {
			message = "Unknown Black Forest Labs error"
		}
	}

	perr := providererrors.NewProviderErrorFromResponse("bfl", 0, "", message, cause)
	// TS APICallError.data: the zod output of bflErrorSchema (message,
	// detail, status, state when present; undeclared keys stripped).
	var obj map[string]interface{}
	if securejson.Unmarshal(body, &obj) == nil {
		data := map[string]interface{}{}
		for _, k := range []string{"message", "detail", "status", "state"} {
			if v, ok := obj[k]; ok {
				data[k] = v
			}
		}
		perr.Data = data
	}
	if bflTerminalTaskStatus(taskStatus) {
		notRetryable := false
		perr.Retryable = &notRetryable
	}
	return perr
}

// bflOffSchemaError is the error for a body that does not match
// bflErrorSchema: the HTTP status text and no data (TS
// createJsonErrorResponseHandler's fallback).
func bflOffSchemaError(statusCode int, cause error) error {
	perr := providererrors.NewProviderErrorFromResponse("bfl", 0, "", http.StatusText(statusCode), cause)
	perr.Data = nil
	return perr
}
