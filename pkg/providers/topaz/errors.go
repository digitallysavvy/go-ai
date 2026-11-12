package topaz

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	Errors    []*topazIssue   `json:"errors"`
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
	if len(trimmed) == 0 || trimmed[0] != '{' || securejson.Unmarshal(body, &data) != nil {
		return topazErrorData{}, false
	}
	// detail: string | array of loose objects with a nullish string msg.
	if len(data.Detail) > 0 && string(bytes.TrimSpace(data.Detail)) != "null" {
		var asString string
		var asIssues []*topazIssue
		if json.Unmarshal(data.Detail, &asString) != nil && !validTopazIssues(data.Detail, &asIssues) {
			return topazErrorData{}, false
		}
	}
	// errors: array of loose objects (a null entry is not an object).
	for _, issue := range data.Errors {
		if issue == nil {
			return topazErrorData{}, false
		}
	}
	return data, true
}

// validTopazIssues reports whether raw is an array of objects (z.object
// rejects null and non-objects), filling issues.
func validTopazIssues(raw json.RawMessage, issues *[]*topazIssue) bool {
	if json.Unmarshal(raw, issues) != nil {
		return false
	}
	for _, issue := range *issues {
		if issue == nil {
			return false
		}
	}
	return true
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
		if issue != nil && issue.Msg != nil {
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

		var asIssues []*topazIssue
		if err := json.Unmarshal(data.Detail, &asIssues); err == nil {
			var messages []string
			for _, issue := range asIssues {
				if issue != nil && issue.Msg != nil {
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
	cause := &internalhttp.HTTPStatusError{StatusCode: statusCode, Headers: headers, Body: body}
	perr := providererrors.NewProviderErrorFromResponse("topaz", 0, errorCode, message, cause)
	// TS APICallError.data: the topazErrorDataSchema output (the declared
	// keys present; loose issue objects kept) for a matching body, else
	// undefined.
	perr.Data = nil
	if ok {
		var raw map[string]interface{}
		_ = securejson.Unmarshal(body, &raw)
		perr.Data = providerutils.PickSchemaKeys(raw, "detail", "message", "error", "errorCode", "errors")
	}
	return perr
}

// Error is a Topaz error TS throws as an AISDKError with a name
// (TOPAZ_IMAGE_ENHANCEMENT_FAILED, TOPAZ_IMAGE_ENHANCEMENT_TIMEOUT). Error()
// is the message, as the TS error's message.
type Error struct {
	// Name is the AISDKError name.
	Name    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// ErrorName is the TS error name, used where TS shows error.toString().
func (e *Error) ErrorName() string { return e.Name }

// topazStringField reports whether the JSON object body has key with a
// non-null value (TS `value == null` is false), and returns the parsed body
// for an InvalidResponseDataError.
func topazStringField(body []byte, key string) (parsed interface{}, present bool) {
	_ = securejson.Unmarshal(body, &parsed)
	obj, _ := parsed.(map[string]interface{})
	v, ok := obj[key]
	return parsed, ok && v != nil
}

// topazSchemaKeys lists the keys a zod object schema declares: "" is the
// root, a dotted path a nested object (topazVideoStatusResponseSchema's
// estimates and download).
type topazSchemaKeys map[string][]string

var (
	// topazImageSubmitKeys is topazImageSubmitResponseSchema.
	topazImageSubmitKeys = topazSchemaKeys{"": {"process_id"}}
	// topazImageDownloadKeys is topazImageDownloadResponseSchema.
	topazImageDownloadKeys = topazSchemaKeys{"": {"download_url"}}
	// topazVideoStatusKeys is topazVideoStatusResponseSchema.
	topazVideoStatusKeys = topazSchemaKeys{
		"":          {"status", "message", "errorCode", "outputSize", "estimates", "download"},
		"estimates": {"cost", "time"},
		"download":  {"url", "expiresAt"},
	}
)

// topazParsedData is the zod output of an already validated response (the
// value TS passes as InvalidResponseDataError data): z.object strips the
// undeclared keys at every level; present keys keep their values, null
// included.
func topazParsedData(v interface{}, keys topazSchemaKeys) interface{} {
	return topazStrip(v, "", keys)
}

func topazStrip(v interface{}, path string, keys topazSchemaKeys) interface{} {
	obj, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	out := map[string]interface{}{}
	for _, key := range keys[path] {
		value, present := obj[key]
		if !present {
			continue
		}
		child := key
		if path != "" {
			child = path + "." + key
		}
		if _, nested := keys[child]; nested {
			value = topazStrip(value, child, keys)
		}
		out[key] = value
	}
	return out
}
