package topaz

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// topazErrorData mirrors the shapes Topaz uses to report errors: the image
// API reports `{ "message": ... }` and the video API `{ "message": ...,
// "errorCode": ... }`. `detail` (a message or FastAPI's array of validation
// issues) and `error` are accepted as well because a few endpoints use them
// instead.
type topazErrorData struct {
	Detail    json.RawMessage `json:"detail"`
	Message   *string         `json:"message"`
	Error     *string         `json:"error"`
	ErrorCode *string         `json:"errorCode"`
	Errors    []topazIssue    `json:"errors"`
}

type topazIssue struct {
	Msg *string `json:"msg"`
}

// parseTopazErrorData decodes a Topaz error response body against the TS
// topazErrorDataSchema. ok is false for a body that does not match it (empty,
// not a JSON object, a wrongly typed field), in which case TS
// createJsonErrorResponseHandler uses the HTTP status text as the message.
func parseTopazErrorData(body []byte) (data topazErrorData, ok bool) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(body, &data) != nil {
		return topazErrorData{}, false
	}
	// detail: string | array of loose objects with a nullish string msg.
	if len(data.Detail) > 0 && string(bytes.TrimSpace(data.Detail)) != "null" {
		var asString string
		var asIssues []topazIssue
		if json.Unmarshal(data.Detail, &asString) != nil && json.Unmarshal(data.Detail, &asIssues) != nil {
			return topazErrorData{}, false
		}
	}
	return data, true
}

// topazErrorToMessage renders a Topaz error response body into a
// human-readable message, mirroring TS topazErrorToMessage.
func topazErrorToMessage(data topazErrorData) string {
	message := topazBaseErrorMessage(data)
	withCode := message
	if data.ErrorCode != nil {
		withCode = message + " (" + *data.ErrorCode + ")"
	}

	// The video API lists field validation failures separately.
	var issues []string
	for _, issue := range data.Errors {
		if issue.Msg != nil {
			issues = append(issues, *issue.Msg)
		}
	}

	if len(issues) > 0 {
		return withCode + ": " + strings.Join(issues, "; ")
	}
	return withCode
}

func topazBaseErrorMessage(data topazErrorData) string {
	if len(data.Detail) > 0 && string(data.Detail) != "null" {
		var asString string
		if err := json.Unmarshal(data.Detail, &asString); err == nil {
			return asString
		}

		var asIssues []topazIssue
		if err := json.Unmarshal(data.Detail, &asIssues); err == nil {
			var messages []string
			for _, issue := range asIssues {
				if issue.Msg != nil {
					messages = append(messages, *issue.Msg)
				}
			}
			if len(messages) > 0 {
				return strings.Join(messages, "; ")
			}
		}
	}

	if data.Message != nil {
		return *data.Message
	}
	if data.Error != nil {
		return *data.Error
	}
	return "Unknown Topaz API error"
}

// newTopazAPIError converts a failed Topaz API response into a provider
// error, mirroring TS topazFailedResponseHandler.
func newTopazAPIError(statusCode int, body []byte, headers http.Header) error {
	data, ok := parseTopazErrorData(body)
	message := http.StatusText(statusCode)
	if ok {
		message = topazErrorToMessage(data)
	}
	errorCode := ""
	if data.ErrorCode != nil {
		errorCode = *data.ErrorCode
	}
	err := providererrors.NewProviderError("topaz", statusCode, errorCode, message, nil)
	err.ResponseBody = string(body)
	if headers != nil {
		err.ResponseHeaders = map[string]string{}
		for k, vals := range headers {
			if len(vals) > 0 {
				err.ResponseHeaders[k] = vals[0]
			}
		}
	}
	return err
}
