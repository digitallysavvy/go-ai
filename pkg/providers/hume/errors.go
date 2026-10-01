package hume

import (
	"encoding/json"
	"errors"
	"fmt"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// humeErrorPayload documents Hume's error envelope: {"error":{"message","code"}}.
type humeErrorPayload struct {
	Error struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// handleError converts an HTTP error into a ProviderError, parsing Hume's
// `{"error":{"message","code"}}` error shape when present.
func handleError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		var payload humeErrorPayload
		message := string(statusErr.Body)
		code := ""
		if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr == nil && payload.Error.Message != "" {
			message = payload.Error.Message
			code = fmt.Sprintf("%d", payload.Error.Code)
		}
		providerErr := providererrors.NewProviderError("hume", statusErr.StatusCode, code, message, err)
		providerErr.ResponseHeaders = providerutils.ExtractHeaders(statusErr.Headers)
		providerErr.ResponseBody = string(statusErr.Body)
		return providerErr
	}
	return providererrors.NewProviderError("hume", 0, "", err.Error(), err)
}
