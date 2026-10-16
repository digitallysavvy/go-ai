package huggingface

import (
	"encoding/json"
	"errors"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// hfErrorPayload mirrors TS huggingfaceErrorDataSchema:
// { error: { message: string, type?: string, code?: string } }.
type hfErrorPayload struct {
	Error *struct {
		Message *string `json:"message"`
		Type    *string `json:"type"`
		Code    *string `json:"code"`
	} `json:"error"`
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
	var payload hfErrorPayload
	if unmarshalErr := json.Unmarshal(statusErr.Body, &payload); unmarshalErr == nil &&
		payload.Error != nil && payload.Error.Message != nil {
		message = *payload.Error.Message
		if payload.Error.Code != nil {
			code = *payload.Error.Code
		}
	}

	return &providererrors.ProviderError{
		Provider:        "huggingface",
		StatusCode:      statusErr.StatusCode,
		ErrorCode:       code,
		Message:         message,
		ResponseHeaders: providerutils.ExtractHeaders(statusErr.Headers),
		ResponseBody:    string(statusErr.Body),
		Cause:           err,
	}
}
