package huggingface

import (
	"encoding/json"
	"errors"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// hfErrorPayload mirrors TS huggingfaceErrorDataSchema:
// { error: { message: string, type?: string, code?: string } }.
type hfErrorPayload struct {
	Error *struct {
		Message *string         `json:"message"`
		Type    json.RawMessage `json:"type"`
		Code    json.RawMessage `json:"code"`
	} `json:"error"`
}

// optionalString reports whether raw is absent or a JSON string: zod
// `z.string().optional()` rejects an explicit null.
func optionalString(raw json.RawMessage) (value string, ok bool) {
	if len(raw) == 0 {
		return "", true
	}
	if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

// parseHuggingFaceError converts a non-2xx HTTP response into a
// *providererrors.ProviderError, mirroring TS huggingfaceFailedResponseHandler
// (createJsonErrorResponseHandler): a body matching the error schema gives
// its message, any other body gives the HTTP status text. Returns nil only
// when err does not carry an *internalhttp.HTTPStatusError.
func parseHuggingFaceError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return nil
	}

	message := http.StatusText(statusErr.StatusCode)
	code := ""
	var data interface{}
	var payload hfErrorPayload
	if unmarshalErr := securejson.Unmarshal(statusErr.Body, &payload); unmarshalErr == nil &&
		payload.Error != nil && payload.Error.Message != nil {
		_, typeOK := optionalString(payload.Error.Type)
		errorCode, codeOK := optionalString(payload.Error.Code)
		if typeOK && codeOK {
			message = *payload.Error.Message
			code = errorCode
			var raw map[string]interface{}
			_ = securejson.Unmarshal(statusErr.Body, &raw)
			// TS APICallError.data: the huggingfaceErrorDataSchema output.
			data = map[string]interface{}{"error": providerutils.PickSchemaKeys(raw["error"], "message", "type", "code")}
		}
	}

	pe := providererrors.NewProviderErrorFromResponse("huggingface", 0, code, message, err)
	pe.Data = data
	return pe
}
