package fireworks

import (
	"encoding/json"
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	Message *string         `json:"message"`
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
	if jsonErr := securejson.Unmarshal(statusErr.Body, &payload); jsonErr != nil || len(payload.Error) == 0 {
		return nil
	}

	// {"error": "plain string message"}
	var message string
	if jsonErr := json.Unmarshal(payload.Error, &message); jsonErr == nil {
		// TS errorToMessage: the string itself, an empty one included.
		pe := providererrors.NewProviderErrorFromResponse("fireworks", 0, "", message, err)
		pe.Data = map[string]interface{}{"error": message} // TS APICallError.data: the schema output
		return pe
	}

	// {"error": {"object","type","code","message"}}
	var obj fireworksErrorObject
	// TS fireworksErrorSchema: message is a required string ("" is valid).
	if jsonErr := jsonexact.Unmarshal(payload.Error, &obj); jsonErr != nil || obj.Message == nil || !fireworksValidCode(obj.Code) {
		return nil
	}
	pe := providererrors.NewProviderErrorFromResponse("fireworks", 0, fireworksErrorCode(obj.Code), *obj.Message, err)
	var errObj map[string]interface{}
	_ = json.Unmarshal(payload.Error, &errObj)
	// TS APICallError.data: the schema output (undeclared keys stripped).
	pe.Data = map[string]interface{}{"error": providerutils.PickSchemaKeys(errObj, "message", "object", "type", "param", "code")}
	return pe
}

// fireworksValidCode is TS `code: z.union([z.string(), z.number()]).nullish()`.
func fireworksValidCode(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return true
	}
	var s string
	var n json.Number
	return json.Unmarshal(raw, &s) == nil || json.Unmarshal(raw, &n) == nil
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

// fireworksCompatAPIError is TS fireworksErrorStructure for the
// OpenAI-compatible completion model: the Fireworks error envelope's message
// for a matching body, else the status text with no data.
func fireworksCompatAPIError(provider string, err error) (*providererrors.ProviderError, bool) {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return nil, false
	}
	if parsed := parseFireworksProviderError(err); parsed != nil {
		if pe, ok := parsed.(*providererrors.ProviderError); ok {
			pe.Provider = provider
			return pe, true
		}
	}
	return providererrors.NewSchemaProviderError(provider, err, nil), true
}
