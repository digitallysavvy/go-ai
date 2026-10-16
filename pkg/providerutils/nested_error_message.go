package providerutils

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// NestedErrorMessage is the TS createJsonErrorResponseHandler outcome for the
// error schema z.object({ error: z.object({ message: z.string(), code:
// z.number() }) }) that AssemblyAI, ElevenLabs, fal, Gladia, Hume and Rev.ai
// share: a body that matches gives its message (even an empty one) and the
// numeric code as a string; anything else (empty body, not JSON, a missing or
// wrongly typed field) gives the HTTP status text and no code.
func NestedErrorMessage(statusCode int, body []byte) (message, code string) {
	var payload struct {
		Error *struct {
			Message *string  `json:"message"`
			Code    *float64 `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != nil &&
		payload.Error.Message != nil && payload.Error.Code != nil {
		return *payload.Error.Message, strconv.FormatFloat(*payload.Error.Code, 'f', -1, 64)
	}
	return http.StatusText(statusCode), ""
}
