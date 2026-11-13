package cartesia

import (
	"encoding/json"
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// cartesiaErrorPayload documents Cartesia's error envelope:
// {"error_code","title","message","request_id","doc_url"}.
type cartesiaErrorPayload struct {
	ErrorCode *string         `json:"error_code"`
	Title     *string         `json:"title"`
	Message   *string         `json:"message"`
	RequestID *string         `json:"request_id"`
	DocURL    json.RawMessage `json:"doc_url"`
}

// handleError converts an HTTP error into a ProviderError, parsing
// Cartesia's error envelope when present. Matches the TypeScript SDK's
// `${title}: ${message}` message format.
func handleError(err error) error {
	if cause, ok := internalhttp.FetchRejectionCause(err); ok {
		return cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
	}
	if noResp, ok := internalhttp.NoResponseError(err); ok {
		return noResp // TS handleFetchError: an abort or timeout is returned unchanged
	}
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		var payload cartesiaErrorPayload
		// TS cartesiaErrorDataSchema: title, message and request_id are all
		// required strings; a body that does not match gives the HTTP status
		// text.
		message := statusErr.Text()
		var data interface{}
		if jsonErr := securejson.Unmarshal(statusErr.Body, &payload); jsonErr == nil &&
			payload.Title != nil && payload.Message != nil && payload.RequestID != nil && docURLMatchesSchema(payload.DocURL) {
			message = *payload.Title + ": " + *payload.Message
			data = schemaErrorData(statusErr.Body, "error_code", "title", "message", "request_id", "doc_url")
		} else {
			payload = cartesiaErrorPayload{}
		}
		errorCode := ""
		if payload.ErrorCode != nil {
			errorCode = *payload.ErrorCode
		}
		perr := providererrors.NewProviderErrorFromResponse("cartesia", 0, errorCode, message, err)
		// TS APICallError.data: the zod-parsed body (undeclared keys
		// stripped) when it matches the schema, else undefined.
		perr.Data = data
		return perr
	}
	// TS postJsonToApi / getFromApi let a transport failure (an abort, "Cannot
	// connect to API") propagate unchanged.
	return err
}

// docURLMatchesSchema is `doc_url: z.string().optional()`: absent or a string;
// an explicit null (or any other type) fails the schema.
func docURLMatchesSchema(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	if string(raw) == "null" {
		return false
	}
	var s string
	return json.Unmarshal(raw, &s) == nil
}

// schemaErrorData is the zod object output of an error body that matched its
// schema: the declared keys that are present (an explicit null kept), every
// undeclared key stripped.
func schemaErrorData(body []byte, keys ...string) interface{} {
	var obj map[string]interface{}
	if securejson.Unmarshal(body, &obj) != nil || obj == nil {
		return nil
	}
	out := make(map[string]interface{}, len(keys))
	for _, k := range keys {
		if v, ok := obj[k]; ok {
			out[k] = v
		}
	}
	return out
}
