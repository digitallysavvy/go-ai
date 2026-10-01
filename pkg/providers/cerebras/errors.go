package cerebras

import (
	"encoding/json"
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// cerebrasErrorPayload documents the Cerebras API error shape:
// {"message": string, "type": string, "param": any, "code": string}.
type cerebrasErrorPayload struct {
	Message string      `json:"message"`
	Type    string      `json:"type"`
	Param   interface{} `json:"param,omitempty"`
	Code    string      `json:"code,omitempty"`
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
	if err := json.Unmarshal(statusErr.Body, &payload); err != nil || payload.Message == "" {
		return nil
	}
	code := payload.Code
	if code == "" {
		code = payload.Type
	}
	return providererrors.NewProviderError("cerebras", statusErr.StatusCode, code, payload.Message, cause)
}
