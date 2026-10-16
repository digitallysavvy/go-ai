package alibaba

import (
	"encoding/json"
	"errors"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// alibabaChatErrorMessage mirrors TS alibabaFailedResponseHandler
// (alibaba-error.ts): {"error":{"message":string,"code"?:string|null,
// "type"?:string|null}} gives its message and code; any other body (empty,
// not JSON, a missing or wrongly typed field) gives the HTTP status text.
func alibabaChatErrorMessage(statusCode int, body []byte) (message, code string) {
	var payload struct {
		Error *struct {
			Message *string `json:"message"`
			Code    *string `json:"code"`
			Type    *string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != nil && payload.Error.Message != nil {
		if payload.Error.Code != nil {
			code = *payload.Error.Code
		}
		return *payload.Error.Message, code
	}
	return http.StatusText(statusCode), ""
}

// alibabaNativeErrorMessage mirrors TS alibabaVideoFailedResponseHandler and
// alibabaEmbeddingFailedResponseHandler, the DashScope native error shape
// {"code"?:string|null,"message":string,"request_id"?:string|null}; any other
// body gives the HTTP status text.
func alibabaNativeErrorMessage(statusCode int, body []byte) (message, code string) {
	var payload struct {
		Code      *string `json:"code"`
		Message   *string `json:"message"`
		RequestID *string `json:"request_id"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Message != nil {
		if payload.Code != nil {
			code = *payload.Code
		}
		return *payload.Message, code
	}
	return http.StatusText(statusCode), ""
}

// chatStatusError builds the provider error for a non-2xx chat response.
func chatStatusError(statusCode int, headers http.Header, body []byte) error {
	cause := &internalhttp.HTTPStatusError{StatusCode: statusCode, Headers: headers, Body: body}
	message, code := alibabaChatErrorMessage(statusCode, body)
	return providererrors.NewProviderError("alibaba", 0, code, message, cause)
}

// asChatStatusError converts an HTTPStatusError anywhere in err's chain; the
// second result is false when err carries no HTTP response.
func asChatStatusError(err error) (error, bool) {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return nil, false
	}
	return chatStatusError(statusErr.StatusCode, statusErr.Headers, statusErr.Body), true
}
