package heygen

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// heygenErrorBody mirrors the HeyGen error envelope
// {"error":{"code","message","param"}}.
type heygenErrorBody struct {
	Error *struct {
		Code    *string `json:"code"`
		Message *string `json:"message"`
		Param   *string `json:"param"`
	} `json:"error"`
}

// newHeyGenAPIError converts a failed HeyGen API response into a provider
// error, mirroring TS heygenFailedResponseHandler: the message is
// "<message> (<code>) [<param>]".
func newHeyGenAPIError(statusCode int, body []byte, headers http.Header) error {
	// TS createJsonErrorResponseHandler reads response.text() (BOM dropped,
	// invalid UTF-8 replaced).
	body = []byte(textdecode.UTF8(body))
	var parsed heygenErrorBody
	message := ""
	code := ""
	var data map[string]interface{}
	if securejson.Unmarshal(body, &parsed) == nil && parsed.Error != nil && parsed.Error.Message != nil {
		message = *parsed.Error.Message
		// TS data: the parsed error; zod strips the keys the schema does
		// not declare.
		inner := map[string]interface{}{"message": *parsed.Error.Message}
		data = map[string]interface{}{"error": inner}
		if parsed.Error.Code != nil {
			code = *parsed.Error.Code
			message += fmt.Sprintf(" (%s)", code)
		}
		if parsed.Error.Param != nil {
			message += fmt.Sprintf(" [%s]", *parsed.Error.Param)
		}
		var present struct {
			Error map[string]json.RawMessage `json:"error"`
		}
		_ = jsonexact.Unmarshal(body, &present)
		for _, key := range []string{"code", "param"} {
			if raw, ok := present.Error[key]; ok {
				var v interface{}
				_ = json.Unmarshal(raw, &v)
				inner[key] = v // a present null stays null
			}
		}
	} else {
		message = http.StatusText(statusCode)
	}
	cause := &internalhttp.HTTPStatusError{StatusCode: statusCode, Headers: headers, Body: body}
	perr := providererrors.NewProviderErrorFromResponse("heygen", 0, code, message, cause)
	// TS createJsonErrorResponseHandler: data only for a body matching the
	// error schema.
	perr.Data = nil
	if data != nil {
		perr.Data = data
	}
	return perr
}
