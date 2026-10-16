package deepgram

import (
	"encoding/json"
	"errors"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// deepgramErrorPayload documents Deepgram's error envelope:
// {"err_code","err_msg","request_id"}.
type deepgramErrorPayload struct {
	ErrCode   *string         `json:"err_code"`
	ErrMsg    *string         `json:"err_msg"`
	RequestID json.RawMessage `json:"request_id,omitempty"`
}

// handleError converts an HTTP error into a ProviderError, parsing
// Deepgram's `{"err_code","err_msg","request_id"}` error shape when present.
func handleError(providerName string, err error) error {
	if providererrors.IsLoadAPIKeyError(err) || providerutils.IsInvalidJSONResponse(err) {
		return err // TS throws these unwrapped
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		var payload deepgramErrorPayload
		// TS deepgramErrorDataSchema: err_code and err_msg are required
		// strings; anything else gives the HTTP status text.
		message := http.StatusText(statusErr.StatusCode)
		code := ""
		if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr == nil && payload.ErrCode != nil && payload.ErrMsg != nil && requestIDValid(payload.RequestID) {
			message = *payload.ErrMsg
			code = *payload.ErrCode
		} else {
			payload = deepgramErrorPayload{}
		}
		providerErr := providererrors.NewProviderErrorFromResponse(providerName, 0, code, message, err)
		var requestID string
		if payload.RequestID != nil {
			_ = json.Unmarshal(payload.RequestID, &requestID)
		}
		if requestID != "" {
			providerErr.Data = map[string]interface{}{"requestId": requestID}
		}
		return providerErr
	}
	// TS postJsonToApi / getFromApi let a transport failure (an abort, "Cannot
	// connect to API") propagate unchanged.
	return err
}

// requestIDValid is z.string().optional(): absent or a string, never null.
func requestIDValid(raw json.RawMessage) bool {
	if raw == nil {
		return true
	}
	var s string
	return json.Unmarshal(raw, &s) == nil && string(raw) != "null"
}
