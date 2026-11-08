package cerebras

import (
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// cerebrasErrorPayload mirrors TS cerebrasErrorSchema: message, type, param
// and code are all required strings (a body missing one, or with a
// non-string one, is not a Cerebras error), and "" is a valid message.
type cerebrasErrorPayload struct {
	Message *string `json:"message"`
	Type    *string `json:"type"`
	Param   *string `json:"param"`
	Code    *string `json:"code"`
}

func parseCerebrasProviderError(err error) error {
	var providerErr *providererrors.ProviderError
	if errors.As(err, &providerErr) {
		if parsed := parseCerebrasHTTPStatusError(providerErr.Cause, err); parsed != nil {
			return parsed
		}
	}
	if parsed := parseCerebrasHTTPStatusError(err, err); parsed != nil {
		return parsed
	}
	return err
}

func parseCerebrasHTTPStatusError(candidate error, cause error) error {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(candidate, &statusErr) {
		return nil
	}
	var payload cerebrasErrorPayload
	if err := securejson.Unmarshal(statusErr.Body, &payload); err != nil ||
		payload.Message == nil || payload.Type == nil || payload.Param == nil || payload.Code == nil {
		return nil
	}
	code := *payload.Code
	if code == "" {
		code = *payload.Type
	}
	return providererrors.NewProviderError("cerebras", statusErr.StatusCode, code, *payload.Message, cause)
}
