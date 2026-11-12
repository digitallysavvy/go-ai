package zai

import (
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// zaiErrorDetails is TS zaiErrorDetailsSchema: {code?: number | string |
// null, message: string}. It reports false when v does not match.
func zaiErrorDetails(v interface{}) (code interface{}, message string, ok bool) {
	obj, isObj := v.(map[string]interface{})
	if !isObj {
		return nil, "", false
	}
	msg, isString := obj["message"].(string)
	if !isString {
		return nil, "", false
	}
	switch c := obj["code"].(type) {
	case nil, string, float64:
		return c, msg, true
	default:
		return nil, "", false
	}
}

// parseZaiProviderError extracts a Z.AI error envelope from an HTTP error
// response and returns a ProviderError carrying the provider's error code.
// The body must match TS zaiErrorSchema (zai-error.ts), a union tried in
// order: {code, message}, then {error: {code, message}}. Returns nil
// (letting the caller fall back to the status text) when it does not.
func parseZaiProviderError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return nil
	}

	var payload interface{}
	if unmarshalErr := securejson.Unmarshal(statusErr.Body, &payload); unmarshalErr != nil {
		return nil
	}

	// TS APICallError.data: the matching member's output (undeclared keys
	// stripped).
	code, message, ok := zaiErrorDetails(payload)
	data := map[string]interface{}(providerutils.PickSchemaKeys(payload, "code", "message"))
	if !ok {
		obj, _ := payload.(map[string]interface{})
		if code, message, ok = zaiErrorDetails(obj["error"]); !ok {
			return nil
		}
		data = map[string]interface{}{"error": providerutils.PickSchemaKeys(obj["error"], "code", "message")}
	}

	pe := providererrors.NewProviderErrorFromResponse("zai", 0, zaiErrorCode(code), message, err)
	pe.Data = data
	return pe
}

// zaiErrorCode normalizes an error code, which may be a JSON string or
// number, to a string.
func zaiErrorCode(code interface{}) string {
	switch c := code.(type) {
	case string:
		return c
	case float64:
		return providerutils.JSNumberString(c)
	default:
		return ""
	}
}
