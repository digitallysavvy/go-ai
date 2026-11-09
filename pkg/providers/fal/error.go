package fal

import (
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// parseFalErrorMessage extracts the message from a fal.ai error response
// body, matching the TS SDK's falErrorDataSchema / falFailedResponseHandler
// (fal-error.ts): {"error":{"message":string,"code":number}} gives its
// message, any other body gives fallback (callers pass the HTTP status text,
// which is what createJsonErrorResponseHandler uses).
func parseFalErrorMessage(body []byte, fallback string) string {
	message, code := providerutils.NestedErrorMessage(0, body)
	if code == "" && message == "" {
		return fallback
	}
	return message
}

// falErrorData is the zod output of falErrorDataSchema for a body that
// matches it ({error: {message, code}}, undeclared keys stripped), else nil
// (TS APICallError.data is undefined for an off-schema body).
func falErrorData(body []byte) interface{} {
	var obj map[string]interface{}
	if securejson.Unmarshal(body, &obj) != nil {
		return nil
	}
	inner, _ := obj["error"].(map[string]interface{})
	message, okMessage := inner["message"].(string)
	code, okCode := inner["code"].(float64)
	if !okMessage || !okCode {
		return nil
	}
	return map[string]interface{}{"error": map[string]interface{}{"message": message, "code": code}}
}

// falAPIError is TS falFailedResponseHandler for an HTTP error response:
// the schema message (else the status text) and the schema data.
func falAPIError(provider string, statusErr *internalhttp.HTTPStatusError, err error) error {
	message := parseFalErrorMessage(statusErr.Body, http.StatusText(statusErr.StatusCode))
	perr := providererrors.NewProviderErrorFromResponse(provider, 0, "", message, err)
	perr.Data = falErrorData(statusErr.Body)
	return perr
}
