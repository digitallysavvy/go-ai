package alibaba

import (
	"errors"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// alibabaChatErrorMessage mirrors TS alibabaFailedResponseHandler
// (alibaba-error.ts): {"error":{"message":string,"code"?:string|null,
// "type"?:string|null}} gives its message and code; any other body (empty,
// not JSON, a missing or wrongly typed field) gives the HTTP status text.
func alibabaChatErrorMessage(statusCode int, body []byte) (message, code string) {
	body = []byte(textdecode.UTF8(body)) // TS response.text()
	var payload struct {
		Error *struct {
			Message *string `json:"message"`
			Code    *string `json:"code"`
			Type    *string `json:"type"`
		} `json:"error"`
	}
	if err := securejson.Unmarshal(body, &payload); err == nil && payload.Error != nil && payload.Error.Message != nil {
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
	body = []byte(textdecode.UTF8(body)) // TS response.text()
	var payload struct {
		Code      *string `json:"code"`
		Message   *string `json:"message"`
		RequestID *string `json:"request_id"`
	}
	if err := securejson.Unmarshal(body, &payload); err == nil && payload.Message != nil {
		if payload.Code != nil {
			code = *payload.Code
		}
		return *payload.Message, code
	}
	return http.StatusText(statusCode), ""
}

// alibabaChatErrorData is the alibabaErrorDataSchema output for a matching
// body ({error: {message, code, type}}, present keys only), else nil (TS
// APICallError.data).
func alibabaChatErrorData(body []byte) interface{} {
	body = []byte(textdecode.UTF8(body))
	var payload struct {
		Error *struct {
			Message *string `json:"message"`
			Code    *string `json:"code"`
			Type    *string `json:"type"`
		} `json:"error"`
	}
	var raw map[string]interface{}
	if securejson.Unmarshal(body, &payload) != nil || payload.Error == nil || payload.Error.Message == nil || securejson.Unmarshal(body, &raw) != nil {
		return nil
	}
	return map[string]interface{}{"error": providerutils.PickSchemaKeys(raw["error"], "message", "code", "type")}
}

// alibabaNativeErrorData is the DashScope native error schema output
// ({code, message, request_id}, present keys only), else nil.
func alibabaNativeErrorData(body []byte) interface{} {
	body = []byte(textdecode.UTF8(body))
	var payload struct {
		Code      *string `json:"code"`
		Message   *string `json:"message"`
		RequestID *string `json:"request_id"`
	}
	var raw map[string]interface{}
	if securejson.Unmarshal(body, &payload) != nil || payload.Message == nil || securejson.Unmarshal(body, &raw) != nil {
		return nil
	}
	return providerutils.PickSchemaKeys(raw, "code", "message", "request_id")
}

// alibabaNativeStatusError is the provider error of a DashScope native API
// (video, embedding) HTTP failure.
func alibabaNativeStatusError(provider string, statusCode int, body []byte, err error) error {
	message, code := alibabaNativeErrorMessage(statusCode, body)
	pe := providererrors.NewProviderErrorFromResponse(provider, 0, code, message, err)
	pe.Data = alibabaNativeErrorData(body)
	return pe
}

// chatStatusError builds the provider error for a non-2xx chat response.
func chatStatusError(statusCode int, headers http.Header, body []byte) error {
	cause := &internalhttp.HTTPStatusError{StatusCode: statusCode, Headers: headers, Body: body}
	message, code := alibabaChatErrorMessage(statusCode, body)
	pe := providererrors.NewProviderErrorFromResponse("alibaba", 0, code, message, cause)
	pe.Data = alibabaChatErrorData(body)
	return pe
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
