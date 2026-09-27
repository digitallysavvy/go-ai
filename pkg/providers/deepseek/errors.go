package deepseek

import (
	"encoding/json"
	"regexp"
	"strconv"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// deepseekHTTPStatusCode mirrors TS deepseek-chat-language-model.ts's
// getHttpStatusCode: an explicit 3-digit numeric or numeric-string code in
// [400, 599].
func deepseekHTTPStatusCode(code interface{}) (int, bool) {
	switch v := code.(type) {
	case float64:
		n := int(v)
		if float64(n) == v && n >= 400 && n <= 599 {
			return n, true
		}
	case int:
		if v >= 400 && v <= 599 {
			return v, true
		}
	case json.Number:
		if n64, err := v.Int64(); err == nil {
			n := int(n64)
			if n >= 400 && n <= 599 {
				return n, true
			}
		}
	case string:
		if regexp.MustCompile(`^\d{3}$`).MatchString(v) {
			n, _ := strconv.Atoi(v)
			if n >= 400 && n <= 599 {
				return n, true
			}
		}
	}
	return 0, false
}

func deepseekIsRetryableStatusCode(statusCode int) bool {
	return statusCode == 408 || statusCode == 409 || statusCode == 429 || statusCode >= 500
}

// deepseekStreamErrorMetadata mirrors TS deepseek-chat-language-model.ts's
// getDeepSeekStreamErrorMetadata: insufficient_quota is a forced
// (429, non-retryable) special case (even though 429 would otherwise be
// retryable), else an explicit HTTP-status-shaped code wins, else a
// code/type discriminator table, else "unset" ((0, false), meaning the
// caller should not override NewStreamProviderError's own inference).
func deepseekStreamErrorMetadata(code, errType string) (statusCode int, isRetryable bool, ok bool) {
	if code == "insufficient_quota" || errType == "insufficient_quota" {
		return 429, false, true
	}
	if sc, matched := deepseekHTTPStatusCode(code); matched {
		return sc, deepseekIsRetryableStatusCode(sc), true
	}
	for _, discriminator := range []string{code, errType} {
		switch discriminator {
		case "rate_limit_exceeded", "rate_limit_error":
			return 429, true, true
		case "server_error", "api_error", "internal_server_error":
			return 500, true, true
		case "overloaded_error", "service_unavailable":
			return 503, true, true
		case "timeout", "timeout_error":
			return 504, true, true
		case "authentication_error", "invalid_api_key":
			return 401, false, true
		case "permission_error":
			return 403, false, true
		case "not_found_error", "model_not_found":
			return 404, false, true
		case "bad_request", "context_length_exceeded", "invalid_request_error":
			return 400, false, true
		}
	}
	return 0, false, false
}

// newDeepSeekStreamProviderErrorChunk builds a
// *providererrors.StreamProviderError from a mid-stream `error` frame's raw
// {"message","type","code"} payload, mirroring TS
// createDeepSeekStreamError(value.error, value).
func newDeepSeekStreamProviderErrorChunk(raw json.RawMessage) *providererrors.StreamProviderError {
	var payload struct {
		Message string          `json:"message"`
		Type    string          `json:"type"`
		Code    json.RawMessage `json:"code"`
	}
	_ = json.Unmarshal(raw, &payload)
	message := payload.Message
	if message == "" {
		message = deepseekStreamErrorText(raw)
	}
	var code interface{}
	if len(payload.Code) > 0 {
		_ = json.Unmarshal(payload.Code, &code)
	}
	codeStr, _ := code.(string)
	if codeStr == "" {
		if n, ok := code.(float64); ok {
			codeStr = strconv.FormatFloat(n, 'f', -1, 64)
		}
	}
	statusCode, isRetryable, matched := deepseekStreamErrorMetadata(codeStr, payload.Type)
	var data interface{}
	_ = json.Unmarshal(raw, &data)
	var statusPtr *int
	var retryablePtr *bool
	if matched {
		statusPtr = &statusCode
		retryablePtr = &isRetryable
	}
	return providererrors.NewStreamProviderError(message, "deepseek", payload.Type, code, statusPtr, retryablePtr, data)
}
