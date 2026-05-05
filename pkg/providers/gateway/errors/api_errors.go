package errors

import (
	"encoding/json"
	"fmt"
)

type GatewayAuthenticationError struct {
	baseGatewayError
}

func NewGatewayAuthenticationError(message string, statusCode int, cause error, generationID string) *GatewayAuthenticationError {
	if message == "" {
		message = "Authentication failed"
	}
	if statusCode == 0 {
		statusCode = 401
	}
	return &GatewayAuthenticationError{
		baseGatewayError: baseGatewayError{
			message:      message,
			statusCode:   statusCode,
			errorType:    "authentication_error",
			cause:        cause,
			generationID: generationID,
		},
	}
}

func CreateContextualAuthenticationError(apiKeyProvided, oidcTokenProvided bool, statusCode int, cause error, generationID string) *GatewayAuthenticationError {
	var message string
	switch {
	case apiKeyProvided:
		message = "AI Gateway authentication failed: Invalid API key.\n\nCreate a new API key: https://vercel.com/d?to=%2F%5Bteam%5D%2F%7E%2Fai%2Fapi-keys\n\nProvide via 'apiKey' option or 'AI_GATEWAY_API_KEY' environment variable."
	case oidcTokenProvided:
		message = "AI Gateway authentication failed: Invalid OIDC token.\n\nRun 'npx vercel link' to link your project, then 'vc env pull' to fetch the token.\n\nAlternatively, use an API key: https://vercel.com/d?to=%2F%5Bteam%5D%2F%7E%2Fai%2Fapi-keys"
	default:
		message = "AI Gateway authentication failed: No authentication provided.\n\nOption 1 - API key:\nCreate an API key: https://vercel.com/d?to=%2F%5Bteam%5D%2F%7E%2Fai%2Fapi-keys\nProvide via 'apiKey' option or 'AI_GATEWAY_API_KEY' environment variable.\n\nOption 2 - OIDC token:\nRun 'npx vercel link' to link your project, then 'vc env pull' to fetch the token."
	}
	return NewGatewayAuthenticationError(message, statusCode, cause, generationID)
}

type GatewayInvalidRequestError struct{ baseGatewayError }

func NewGatewayInvalidRequestError(message string, statusCode int, cause error, generationID string) *GatewayInvalidRequestError {
	if message == "" {
		message = "Invalid request"
	}
	if statusCode == 0 {
		statusCode = 400
	}
	return &GatewayInvalidRequestError{baseGatewayError{message: message, statusCode: statusCode, errorType: "invalid_request_error", cause: cause, generationID: generationID}}
}

type GatewayRateLimitError struct{ baseGatewayError }

func NewGatewayRateLimitError(message string, statusCode int, cause error, generationID string) *GatewayRateLimitError {
	if message == "" {
		message = "Rate limit exceeded"
	}
	if statusCode == 0 {
		statusCode = 429
	}
	return &GatewayRateLimitError{baseGatewayError{message: message, statusCode: statusCode, errorType: "rate_limit_exceeded", cause: cause, generationID: generationID}}
}

type GatewayModelNotFoundError struct {
	baseGatewayError
	ModelID string
}

func NewGatewayModelNotFoundError(message string, statusCode int, modelID string, cause error, generationID string) *GatewayModelNotFoundError {
	if message == "" {
		message = "Model not found"
	}
	if statusCode == 0 {
		statusCode = 404
	}
	return &GatewayModelNotFoundError{
		baseGatewayError: baseGatewayError{message: message, statusCode: statusCode, errorType: "model_not_found", cause: cause, generationID: generationID},
		ModelID:          modelID,
	}
}

type GatewayInternalServerError struct{ baseGatewayError }

func NewGatewayInternalServerError(message string, statusCode int, cause error, generationID string) *GatewayInternalServerError {
	if message == "" {
		message = "Internal server error"
	}
	if statusCode == 0 {
		statusCode = 500
	}
	return &GatewayInternalServerError{baseGatewayError{message: message, statusCode: statusCode, errorType: "internal_server_error", cause: cause, generationID: generationID}}
}

type GatewayResponseError struct {
	baseGatewayError
	Response        interface{}
	ValidationError error
}

func NewGatewayResponseError(message string, statusCode int, response interface{}, validationError error, cause error, generationID string) *GatewayResponseError {
	if message == "" {
		message = "Invalid response from Gateway"
	}
	if statusCode == 0 {
		statusCode = 502
	}
	return &GatewayResponseError{
		baseGatewayError: baseGatewayError{message: message, statusCode: statusCode, errorType: "response_error", cause: cause, generationID: generationID},
		Response:         response,
		ValidationError:  validationError,
	}
}

type gatewayErrorPayload struct {
	Error *struct {
		Message string          `json:"message"`
		Type    string          `json:"type"`
		Param   json.RawMessage `json:"param"`
	} `json:"error"`
	GenerationID string `json:"generationId"`
}

type modelNotFoundParam struct {
	ModelID string `json:"modelId"`
}

func CreateGatewayErrorFromResponse(responseBody []byte, statusCode int, defaultMessage string, cause error, authMethod string) error {
	var payload gatewayErrorPayload
	if err := json.Unmarshal(responseBody, &payload); err != nil || payload.Error == nil {
		rawGenerationID := ""
		var fallback map[string]interface{}
		if json.Unmarshal(responseBody, &fallback) == nil {
			if value, ok := fallback["generationId"].(string); ok {
				rawGenerationID = value
			}
			return NewGatewayResponseError(fmt.Sprintf("Invalid error response format: %s", defaultMessage), statusCode, fallback, err, cause, rawGenerationID)
		}
		return NewGatewayResponseError(fmt.Sprintf("Invalid error response format: %s", defaultMessage), statusCode, string(responseBody), err, cause, rawGenerationID)
	}

	generationID := payload.GenerationID
	switch payload.Error.Type {
	case "authentication_error":
		return CreateContextualAuthenticationError(authMethod == "api-key", authMethod == "oidc", statusCode, cause, generationID)
	case "invalid_request_error":
		return NewGatewayInvalidRequestError(payload.Error.Message, statusCode, cause, generationID)
	case "rate_limit_exceeded":
		return NewGatewayRateLimitError(payload.Error.Message, statusCode, cause, generationID)
	case "model_not_found":
		var param modelNotFoundParam
		_ = json.Unmarshal(payload.Error.Param, &param) // best effort
		return NewGatewayModelNotFoundError(payload.Error.Message, statusCode, param.ModelID, cause, generationID)
	case "internal_server_error":
		return NewGatewayInternalServerError(payload.Error.Message, statusCode, cause, generationID)
	default:
		return NewGatewayInternalServerError(payload.Error.Message, statusCode, cause, generationID)
	}
}
