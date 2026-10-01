package deepgram

import (
	"encoding/json"
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// deepgramErrorPayload documents Deepgram's error envelope:
// {"err_code","err_msg","request_id"}.
type deepgramErrorPayload struct {
	ErrCode   string `json:"err_code"`
	ErrMsg    string `json:"err_msg"`
	RequestID string `json:"request_id,omitempty"`
}

// handleError converts an HTTP error into a ProviderError, parsing
// Deepgram's `{"err_code","err_msg","request_id"}` error shape when present.
func handleError(providerName string, err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		var payload deepgramErrorPayload
		message := string(statusErr.Body)
		code := ""
		if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr == nil && payload.ErrMsg != "" {
			message = payload.ErrMsg
			code = payload.ErrCode
		}
		providerErr := providererrors.NewProviderError(providerName, statusErr.StatusCode, code, message, err)
		providerErr.ResponseHeaders = providerutils.ExtractHeaders(statusErr.Headers)
		providerErr.ResponseBody = string(statusErr.Body)
		if payload.RequestID != "" {
			providerErr.Data = map[string]interface{}{"requestId": payload.RequestID}
		}
		return providerErr
	}
	return providererrors.NewProviderError(providerName, 0, "", err.Error(), err)
}
