package huggingface

import (
	"encoding/json"
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// hfErrorPayload mirrors TS huggingfaceErrorDataSchema:
// { error: { message: string, type?: string, code?: string } }.
type hfErrorPayload struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type,omitempty"`
		Code    string `json:"code,omitempty"`
	} `json:"error"`
}

// parseHuggingFaceError converts a non-2xx HTTP response into a
// *providererrors.ProviderError using the Hugging Face error envelope,
// mirroring TS huggingfaceFailedResponseHandler
// (createJsonErrorResponseHandler). Returns nil if err does not carry an
// *internalhttp.HTTPStatusError or the body doesn't match the expected shape.
func parseHuggingFaceError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return nil
	}

	var payload hfErrorPayload
	if unmarshalErr := json.Unmarshal(statusErr.Body, &payload); unmarshalErr != nil || payload.Error.Message == "" {
		return nil
	}

	return &providererrors.ProviderError{
		Provider:        "huggingface",
		StatusCode:      statusErr.StatusCode,
		ErrorCode:       payload.Error.Code,
		Message:         payload.Error.Message,
		ResponseHeaders: providerutils.ExtractHeaders(statusErr.Headers),
		ResponseBody:    string(statusErr.Body),
		Cause:           err,
	}
}
