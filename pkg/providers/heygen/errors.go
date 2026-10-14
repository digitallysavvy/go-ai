package heygen

import (
	"encoding/json"
	"fmt"
	"net/http"

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
	err := providererrors.NewProviderError("heygen", statusCode, code, message, nil)
	err.ResponseBody = string(body)
	if headers != nil {
		err.ResponseHeaders = map[string]string{}
		for k, vals := range headers {
			if len(vals) > 0 {
				err.ResponseHeaders[k] = vals[0]
			}
		}
	}
	return err
}
