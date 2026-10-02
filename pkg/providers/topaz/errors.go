package topaz

import (
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

// parseTopazErrorData decodes a Topaz error response body. A body that
// cannot be decoded as JSON is treated as having no structured fields, so
// the caller falls back to the raw body text.
func parseTopazErrorData(body []byte) topazErrorData {
	var data topazErrorData
	_ = json.Unmarshal(body, &data)
	return data
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
	data := parseTopazErrorData(body)
	message := topazErrorToMessage(data)
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
