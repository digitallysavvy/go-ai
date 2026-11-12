package tencent

import (
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// numberOrString is z.union([z.number(), z.string()]).
var numberOrString = zodlite.Union(zodlite.Number, zodlite.String)

// tokenHubErrorDataSchema is TS tencentTokenHubErrorDataSchema: the flat
// {code, message, request_id?} envelope or the OpenAI-style {error: {...}}.
var tokenHubErrorDataSchema = zodlite.Union(
	zodlite.Object(
		zodlite.F("code", numberOrString),
		zodlite.F("message", zodlite.String),
		zodlite.F("request_id", zodlite.Nullish(zodlite.String)),
	),
	zodlite.Object(
		zodlite.F("error", zodlite.Object(
			zodlite.F("message", zodlite.String),
			zodlite.F("type", zodlite.Nullish(zodlite.String)),
			zodlite.F("code", zodlite.Nullish(numberOrString)),
		)),
	),
)

// tokenHubErrorData is the zod output of tokenHubErrorDataSchema for a body
// that matched it (the first matching union member, undeclared keys
// stripped), and the message TS errorToMessage picks.
func tokenHubErrorData(tree map[string]interface{}) (map[string]interface{}, string) {
	flat := zodlite.Object(
		zodlite.F("code", numberOrString),
		zodlite.F("message", zodlite.String),
		zodlite.F("request_id", zodlite.Nullish(zodlite.String)),
	)
	if flat(tree, true, nil) == nil {
		data := map[string]interface{}{"code": tree["code"], "message": tree["message"]}
		if v, ok := tree["request_id"]; ok {
			data["request_id"] = v
		}
		message, _ := tree["message"].(string)
		return data, message
	}
	e, _ := tree["error"].(map[string]interface{})
	inner := map[string]interface{}{"message": e["message"]}
	for _, k := range []string{"type", "code"} {
		if v, ok := e[k]; ok {
			inner[k] = v
		}
	}
	message, _ := e["message"].(string)
	return map[string]interface{}{"error": inner}, message
}

// tokenHubFailedResponse is TS tencentTokenHubFailedResponseHandler for a
// non-2xx response: the message and data of a body matching the error
// schema, else the HTTP status text with no data.
func tokenHubFailedResponse(statusCode int, headers http.Header, body []byte) error {
	cause := &internalhttp.HTTPStatusError{StatusCode: statusCode, Headers: headers, Body: body}
	message := http.StatusText(statusCode)
	var data interface{}
	var tree interface{}
	if securejson.Unmarshal(body, &tree) == nil && tokenHubErrorDataSchema(tree, true, nil) == nil {
		var obj map[string]interface{}
		obj, message = tokenHubErrorData(tree.(map[string]interface{}))
		data = obj
	}
	perr := providererrors.NewProviderErrorFromResponse("tencent", 0, "", message, cause)
	perr.Data = data
	return perr
}

// tokenHubResponse validates a successful TokenHub response (TS
// createTencentTokenHubResponseHandler): the body must match schema, and a
// Kling envelope with a non-zero code is a failed request even with a 2xx
// status (an APICallError that is not retryable). It returns the parsed
// body, reduced by strip to the zod output (undeclared keys removed).
func tokenHubResponse(statusCode int, headers http.Header, body []byte, schema zodlite.Node, strip func(map[string]interface{}) map[string]interface{}) (map[string]interface{}, error) {
	var tree interface{}
	if err := securejson.Unmarshal(body, &tree); err != nil {
		return nil, providerutils.InvalidJSONResponseError("tencent", statusCode, headers, body, err)
	}
	if issue := schema(tree, true, nil); issue != nil {
		return nil, providerutils.InvalidJSONResponseError("tencent", statusCode, headers, body, issue)
	}
	raw := tree.(map[string]interface{})
	value := strip(raw)
	code := value["code"]
	failed := false
	var codeText string
	switch c := code.(type) {
	case float64:
		failed = c != 0
		codeText = providerutils.JSNumberString(c)
	case string:
		failed = c != "0"
		codeText = c
	}
	if failed {
		message := "TokenHub request failed with code " + codeText
		if m, ok := value["message"].(string); ok {
			message = m
		}
		notRetryable := false
		return nil, &providererrors.ProviderError{
			Provider:        "tencent",
			StatusCode:      statusCode,
			Message:         message,
			ResponseHeaders: providerutils.ExtractHeaders(headers),
			ResponseBody:    jsonorder.Text(body, raw),
			Data:            value,
			Retryable:       &notRetryable,
		}
	}
	return value, nil
}
