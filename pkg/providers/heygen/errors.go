package heygen

import (
	"encoding/json"
	"fmt"
	"net/http"

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
	var parsed heygenErrorBody
	message := ""
	code := ""
	if json.Unmarshal(body, &parsed) == nil && parsed.Error != nil && parsed.Error.Message != nil {
		message = *parsed.Error.Message
		if parsed.Error.Code != nil {
			code = *parsed.Error.Code
			message += fmt.Sprintf(" (%s)", code)
		}
		if parsed.Error.Param != nil {
			message += fmt.Sprintf(" [%s]", *parsed.Error.Param)
		}
	} else {
		message = http.StatusText(statusCode)
	}
	cause := &internalhttp.HTTPStatusError{StatusCode: statusCode, Headers: headers, Body: body}
	return providererrors.NewProviderErrorFromResponse("heygen", 0, code, message, cause)
}
