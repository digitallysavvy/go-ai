package providerutils

import (
	"encoding/json"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
)

// NestedErrorMessage is the TS createJsonErrorResponseHandler outcome for the
// error schema z.object({ error: z.object({ message: z.string(), code:
// z.number() }) }) that AssemblyAI, ElevenLabs, fal, Gladia, Hume and Rev.ai
// share: a body that matches gives its message (even an empty one) and the
// numeric code as JS number text (String(code): 1e21 is "1e+21"); anything
// else (empty body, not JSON, a missing, wrongly typed or differently cased
// field) gives the HTTP status text and no code.
func NestedErrorMessage(statusCode int, body []byte) (message, code string) {
	return NestedErrorMessageText(http.StatusText(statusCode), body)
}

// NestedErrorMessageText is NestedErrorMessage with the response's status
// text (TS response.statusText, the server reason phrase) as the fallback.
func NestedErrorMessageText(statusText string, body []byte) (message, code string) {
	// Decoded through raw maps, not a struct, so the keys match exactly as
	// zod's do (encoding/json would also match "Error" or "MESSAGE"), and a
	// number out of the double range in another field (JS Infinity, which
	// zod's object strips) does not fail the decode.
	var payload map[string]json.RawMessage
	var errObj map[string]json.RawMessage
	var msg string
	var num float64
	if securejson.Unmarshal(body, &payload) == nil &&
		json.Unmarshal(payload["error"], &errObj) == nil && errObj != nil &&
		jsonExact(errObj["message"], &msg) && jsonExact(errObj["code"], &num) {
		return msg, JSNumberString(num)
	}
	return statusText, ""
}

// jsonExact decodes a present, non-null raw value into v (a string or float64
// target fails on any other JSON type).
func jsonExact(raw json.RawMessage, v interface{}) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}
