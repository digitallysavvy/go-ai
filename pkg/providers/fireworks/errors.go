package fireworks

import (
	"encoding/json"
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// fireworksErrorPayload documents Fireworks' error envelope. TS's
// fireworksErrorSchema types `error` as `z.union([z.string(), z.object({...})])`
// and its `code` as `z.union([z.string(), z.number()])`; both are decoded as
// json.RawMessage here so a plain-string error or a numeric code don't fail
// the whole unmarshal (which would otherwise silently drop the parsed
// message entirely and fall back to a generic handler).
type fireworksErrorPayload struct {
	Error json.RawMessage `json:"error"`
}

type fireworksErrorObject struct {
	Object  string          `json:"object,omitempty"`
	Type    string          `json:"type,omitempty"`
	Code    json.RawMessage `json:"code,omitempty"`
	Message string          `json:"message"`
}

// parseFireworksProviderError unwraps an HTTPStatusError and, when the body
// matches Fireworks' error envelope (an object error with a message, or a
// bare string error), returns a ProviderError carrying that message (and
// code, when present) instead of the raw response body text.
func parseFireworksProviderError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return nil
	}
	var payload fireworksErrorPayload
	if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr != nil || len(payload.Error) == 0 {
		return nil
	}

	// {"error": "plain string message"}
	var message string
	if jsonErr := json.Unmarshal(payload.Error, &message); jsonErr == nil {
		if message == "" {
			return nil
		}
		return providererrors.NewProviderError("fireworks", statusErr.StatusCode, "", message, err)
	}

	// {"error": {"object","type","code","message"}}
	var obj fireworksErrorObject
	if jsonErr := json.Unmarshal(payload.Error, &obj); jsonErr != nil || obj.Message == "" {
		return nil
	}
	return providererrors.NewProviderError("fireworks", statusErr.StatusCode, fireworksErrorCode(obj.Code), obj.Message, err)
}

// fireworksErrorCode stringifies a code that may be a JSON string or number.
func fireworksErrorCode(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	return ""
}
