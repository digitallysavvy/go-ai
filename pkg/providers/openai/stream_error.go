package openai

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

type openAIStreamErrorFrame struct {
	Message string
	Code    interface{}
	Type    string
	Frame   interface{}
}

func newOpenAIStreamProviderError(providerName string, raw json.RawMessage, headers http.Header) error {
	frame := parseOpenAIStreamError(raw)
	message := "OpenAI stream failed before any output was generated"
	statusCode := 500
	errorCode := ""
	var data interface{}

	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &data)
	}
	if frame != nil {
		if frame.Message != "" {
			message = frame.Message
		}
		statusCode = openAIStreamErrorStatusCode(frame)
		errorCode = openAIStreamErrorCodeString(frame.Code)
	}

	return &providererrors.ProviderError{
		Provider:        providerName,
		StatusCode:      statusCode,
		ErrorCode:       errorCode,
		Message:         message,
		ResponseHeaders: providerutils.ExtractHeaders(headers),
		ResponseBody:    string(raw),
		Data:            data,
	}
}

func parseOpenAIStreamError(raw json.RawMessage) *openAIStreamErrorFrame {
	var value map[string]interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}

	if valueType, _ := value["type"].(string); valueType == "response.failed" {
		response, _ := value["response"].(map[string]interface{})
		responseError, _ := response["error"].(map[string]interface{})
		message, _ := responseError["message"].(string)
		if message == "" {
			return nil
		}
		return &openAIStreamErrorFrame{
			Message: message,
			Code:    responseError["code"],
			Type:    "response.failed",
			Frame:   value,
		}
	}

	errorValue, _ := value["error"].(map[string]interface{})
	if errorValue == nil {
		errorValue = value
	}
	message, _ := errorValue["message"].(string)
	if message == "" {
		return nil
	}
	if _, hasNestedError := value["error"].(map[string]interface{}); !hasNestedError {
		_, hasCode := errorValue["code"]
		errorType, hasType := errorValue["type"].(string)
		_, hasParam := errorValue["param"]
		if !hasCode && !hasParam && (!hasType || errorType == "") {
			return nil
		}
	}
	errorType, _ := errorValue["type"].(string)
	return &openAIStreamErrorFrame{
		Message: message,
		Code:    errorValue["code"],
		Type:    errorType,
		Frame:   value,
	}
}

func openAIStreamErrorStatusCode(frame *openAIStreamErrorFrame) int {
	if frame == nil {
		return 500
	}
	switch code := frame.Code.(type) {
	case float64:
		if status := int(code); float64(status) == code && isHTTPErrorStatusCode(status) {
			return status
		}
	case int:
		if isHTTPErrorStatusCode(code) {
			return code
		}
	case json.Number:
		if status64, err := code.Int64(); err == nil && isHTTPErrorStatusCode(int(status64)) {
			return int(status64)
		}
	case string:
		if regexp.MustCompile(`^\d{3}$`).MatchString(code) {
			status, _ := strconv.Atoi(code)
			if isHTTPErrorStatusCode(status) {
				return status
			}
		}
	}

	discriminator := strings.ToLower(strings.TrimSpace(openAIStreamErrorCodeString(frame.Code) + " " + frame.Type))
	if strings.Contains(discriminator, "insufficient_quota") || strings.Contains(discriminator, "rate_limit") {
		return 429
	}
	if strings.Contains(discriminator, "authentication") {
		return 401
	}
	if strings.Contains(discriminator, "permission") {
		return 403
	}
	if strings.Contains(discriminator, "not_found") {
		return 404
	}
	if strings.Contains(discriminator, "invalid") || strings.Contains(discriminator, "bad_request") || strings.Contains(discriminator, "context_length") {
		return 400
	}
	if strings.Contains(discriminator, "overload") {
		return 503
	}
	if strings.Contains(discriminator, "timeout") {
		return 504
	}
	return 500
}

func openAIStreamErrorCodeString(code interface{}) string {
	switch v := code.(type) {
	case string:
		return v
	case float64:
		if float64(int(v)) == v {
			return strconv.Itoa(int(v))
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func isHTTPErrorStatusCode(status int) bool {
	return status >= 400 && status <= 599
}
