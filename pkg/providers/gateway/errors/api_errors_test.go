package errors

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestGatewayAuthenticationErrorDefaults(t *testing.T) {
	cause := errors.New("cause")
	err := NewGatewayAuthenticationError("", 0, cause, "gen_1")
	if err.GetStatusCode() != 401 {
		t.Fatalf("status = %d, want 401", err.GetStatusCode())
	}
	if err.GetType() != "authentication_error" {
		t.Fatalf("type = %q", err.GetType())
	}
	if err.GetGenerationID() != "gen_1" {
		t.Fatalf("generationID = %q", err.GetGenerationID())
	}
	if err.Unwrap() != cause {
		t.Fatal("expected cause in Unwrap")
	}
	if !strings.Contains(err.Error(), "Authentication failed") || !strings.Contains(err.Error(), "[gen_1]") {
		t.Fatalf("Error() = %q", err.Error())
	}
	if !IsGatewayError(err) {
		t.Fatal("expected IsGatewayError=true")
	}
}

func TestCreateContextualAuthenticationError(t *testing.T) {
	apiKeyErr := CreateContextualAuthenticationError(true, false, 401, nil, "")
	if !strings.Contains(apiKeyErr.Error(), "Invalid API key") {
		t.Fatalf("apiKey message = %q", apiKeyErr.Error())
	}

	oidcErr := CreateContextualAuthenticationError(false, true, 401, nil, "")
	if !strings.Contains(oidcErr.Error(), "Invalid OIDC token") {
		t.Fatalf("oidc message = %q", oidcErr.Error())
	}

	noneErr := CreateContextualAuthenticationError(false, false, 401, nil, "")
	if !strings.Contains(noneErr.Error(), "No authentication provided") {
		t.Fatalf("none message = %q", noneErr.Error())
	}
}

func TestGatewayAPIErrorConstructorsDefaults(t *testing.T) {
	cause := errors.New("boom")
	invalid := NewGatewayInvalidRequestError("", 0, cause, "")
	if invalid.GetStatusCode() != 400 || invalid.GetType() != "invalid_request_error" {
		t.Fatalf("invalid request error mismatch: %#v", invalid)
	}

	rate := NewGatewayRateLimitError("", 0, cause, "")
	if rate.GetStatusCode() != 429 || rate.GetType() != "rate_limit_exceeded" {
		t.Fatalf("rate limit error mismatch: %#v", rate)
	}

	notFound := NewGatewayModelNotFoundError("", 0, "openai/gpt-5", cause, "gid")
	if notFound.GetStatusCode() != 404 || notFound.GetType() != "model_not_found" {
		t.Fatalf("model not found mismatch: %#v", notFound)
	}
	if notFound.ModelID != "openai/gpt-5" {
		t.Fatalf("model id = %q", notFound.ModelID)
	}

	internal := NewGatewayInternalServerError("", 0, cause, "")
	if internal.GetStatusCode() != 500 || internal.GetType() != "internal_server_error" {
		t.Fatalf("internal error mismatch: %#v", internal)
	}

	response := NewGatewayResponseError("", 0, map[string]interface{}{"x": 1}, cause, cause, "")
	if response.GetStatusCode() != 502 || response.GetType() != "response_error" {
		t.Fatalf("response error mismatch: %#v", response)
	}
	if response.ValidationError == nil || response.Response == nil {
		t.Fatalf("expected response and validation error fields to be populated")
	}
}

func TestGatewayErrorIsRetryableStatusCodes(t *testing.T) {
	tests := []struct {
		statusCode int
		want       bool
	}{
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{408, true},
		{409, true},
		{410, false},
		{428, false},
		{429, true},
		{430, false},
		{499, false},
		{500, true},
		{503, true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.statusCode), func(t *testing.T) {
			err := NewGatewayInternalServerError("status", tt.statusCode, nil, "")
			if got := err.IsRetryable(); got != tt.want {
				t.Fatalf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateGatewayErrorFromResponseTypedCases(t *testing.T) {
	cause := errors.New("origin")

	err := CreateGatewayErrorFromResponse(
		[]byte(`{"error":{"message":"missing model","type":"model_not_found","param":{"modelId":"openai/gpt-4"}},"generationId":"gen_123"}`),
		404,
		"default",
		cause,
		"api-key",
	)
	var modelErr *GatewayModelNotFoundError
	if !errors.As(err, &modelErr) {
		t.Fatalf("error type = %T, want *GatewayModelNotFoundError", err)
	}
	if modelErr.ModelID != "openai/gpt-4" || modelErr.GetGenerationID() != "gen_123" {
		t.Fatalf("model error mismatch: %#v", modelErr)
	}

	err = CreateGatewayErrorFromResponse(
		[]byte(`{"error":{"message":"rate","type":"rate_limit_exceeded","param":null}}`),
		429,
		"default",
		cause,
		"api-key",
	)
	var rateErr *GatewayRateLimitError
	if !errors.As(err, &rateErr) {
		t.Fatalf("error type = %T, want *GatewayRateLimitError", err)
	}

	err = CreateGatewayErrorFromResponse(
		[]byte(`{"error":{"message":"auth","type":"authentication_error","param":null}}`),
		401,
		"default",
		cause,
		"oidc",
	)
	var authErr *GatewayAuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("error type = %T, want *GatewayAuthenticationError", err)
	}
	if !strings.Contains(authErr.Error(), "OIDC token") {
		t.Fatalf("auth error message = %q", authErr.Error())
	}

	var internalErr *GatewayInternalServerError
	err = CreateGatewayErrorFromResponse(
		[]byte(`{"error":{"message":"upstream rate","type":"AI_APICallError","param":{"provider":"x"},"code":"too_many"},"generationId":"gen_rate"}`),
		429,
		"default",
		cause,
		"api-key",
	)
	if !errors.As(err, &internalErr) {
		t.Fatalf("error type = %T, want *GatewayInternalServerError for unknown TS error type", err)
	}
	var details GatewayErrorDetails
	if !errors.As(err, &details) {
		t.Fatalf("error type = %T, want GatewayErrorDetails", err)
	}
	if details.GetRawType() != "AI_APICallError" || details.GetCode() != "too_many" {
		t.Fatalf("raw details = type:%#v code:%#v", details.GetRawType(), details.GetCode())
	}
	if internalErr.GetGenerationID() != "gen_rate" {
		t.Fatalf("generationID = %q", internalErr.GetGenerationID())
	}

	err = CreateGatewayErrorFromResponse(
		[]byte(`{"error":{"message":"upstream bad","type":"AI_APICallError","code":400}}`),
		400,
		"default",
		cause,
		"api-key",
	)
	if !errors.As(err, &internalErr) {
		t.Fatalf("error type = %T, want *GatewayInternalServerError for unknown TS error type", err)
	}
	if !errors.As(err, &details) || details.GetRawType() != "AI_APICallError" || details.GetCode() != float64(400) {
		t.Fatalf("invalid raw details = %#v", details)
	}

	err = CreateGatewayErrorFromResponse(
		[]byte(`{"error":{"message":"unknown","type":"new_type","param":null}}`),
		500,
		"default",
		cause,
		"api-key",
	)
	if !errors.As(err, &internalErr) {
		t.Fatalf("error type = %T, want *GatewayInternalServerError", err)
	}

	for _, rawType := range []string{"timeout"} {
		err = CreateGatewayErrorFromResponse(
			[]byte(`{"error":{"message":"unknown gateway type","type":"`+rawType+`","param":null}}`),
			504,
			"default",
			cause,
			"api-key",
		)
		if !errors.As(err, &internalErr) {
			t.Fatalf("error type for %s = %T, want *GatewayInternalServerError", rawType, err)
		}
		if !errors.As(err, &details) || details.GetRawType() != rawType {
			t.Fatalf("raw details for %s = %#v", rawType, details.GetRawType())
		}
	}

	err = CreateGatewayErrorFromResponse(
		[]byte(`{"error":{"message":"provider missing","type":"failed_dependency","param":null}}`),
		424,
		"default",
		cause,
		"api-key",
	)
	var failedDependencyErr *GatewayFailedDependencyError
	if !errors.As(err, &failedDependencyErr) {
		t.Fatalf("error type = %T, want *GatewayFailedDependencyError", err)
	}
	if failedDependencyErr.IsRetryable() {
		t.Fatal("failed dependency should not be retryable")
	}

	err = CreateGatewayErrorFromResponse(
		[]byte(`{"error":{"message":"blocked","type":"forbidden","code":"rule","param":{"ruleId":"r1"}}}`),
		403,
		"default",
		cause,
		"api-key",
	)
	var forbiddenErr *GatewayForbiddenError
	if !errors.As(err, &forbiddenErr) {
		t.Fatalf("error type = %T, want *GatewayForbiddenError", err)
	}
	if forbiddenErr.IsRetryable() {
		t.Fatal("forbidden should not be retryable")
	}
	if !errors.As(err, &details) || details.GetRawType() != "forbidden" || details.GetCode() != "rule" {
		t.Fatalf("forbidden details = %#v", details)
	}
}

func TestCreateGatewayErrorFromResponseInvalidPayload(t *testing.T) {
	cause := errors.New("origin")

	err := CreateGatewayErrorFromResponse(
		[]byte(`{"generationId":"gen_789","error":"unexpected-string"}`),
		502,
		"fallback",
		cause,
		"api-key",
	)
	var responseErr *GatewayResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("error type = %T, want *GatewayResponseError", err)
	}
	if responseErr.GetGenerationID() != "gen_789" {
		t.Fatalf("generationID = %q, want gen_789", responseErr.GetGenerationID())
	}
	if !strings.Contains(responseErr.Error(), "Invalid error response format") {
		t.Fatalf("Error() = %q", responseErr.Error())
	}

	err = CreateGatewayErrorFromResponse(
		[]byte(`not-json`),
		502,
		"fallback",
		cause,
		"api-key",
	)
	if !errors.As(err, &responseErr) {
		t.Fatalf("error type = %T, want *GatewayResponseError", err)
	}
	if responseErr.ValidationError == nil {
		t.Fatal("expected validation error for non-JSON payload")
	}
}

func TestCreateGatewayErrorFromResponsePreservesMalformedJSONValues(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want interface{}
	}{
		{name: "string", body: []byte(`"Error string"`), want: "Error string"},
		{name: "array", body: []byte(`["error","array"]`), want: []interface{}{"error", "array"}},
		{name: "number", body: []byte(`404`), want: float64(404)},
		{name: "boolean", body: []byte(`true`), want: true},
		{name: "null", body: []byte(`null`), want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CreateGatewayErrorFromResponse(tt.body, 502, "fallback", nil, "api-key")
			var responseErr *GatewayResponseError
			if !errors.As(err, &responseErr) {
				t.Fatalf("error type = %T, want *GatewayResponseError", err)
			}
			if fmt.Sprintf("%#v", responseErr.Response) != fmt.Sprintf("%#v", tt.want) {
				t.Fatalf("response = %#v, want %#v", responseErr.Response, tt.want)
			}
			if responseErr.ValidationError == nil {
				t.Fatal("expected validation error for malformed response")
			}
		})
	}
}

func TestCreateGatewayErrorFromResponseRequiresStringMessage(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{name: "missing", body: []byte(`{"error":{"type":"rate_limit_exceeded"}}`)},
		{name: "null", body: []byte(`{"error":{"message":null,"type":"rate_limit_exceeded"}}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CreateGatewayErrorFromResponse(tt.body, 429, "fallback", nil, "api-key")
			var responseErr *GatewayResponseError
			if !errors.As(err, &responseErr) {
				t.Fatalf("error type = %T, want *GatewayResponseError", err)
			}
			if responseErr.ValidationError == nil {
				t.Fatal("expected validation error for missing/null message")
			}
		})
	}
}

func TestCreateGatewayErrorFromResponsePreservesEmptyTypedMessages(t *testing.T) {
	tests := []struct {
		name       string
		body       []byte
		assertType func(error) bool
	}{
		{
			name: "invalid request",
			body: []byte(`{"error":{"message":"","type":"invalid_request_error"}}`),
			assertType: func(err error) bool {
				var target *GatewayInvalidRequestError
				return errors.As(err, &target)
			},
		},
		{
			name: "rate limit",
			body: []byte(`{"error":{"message":"","type":"rate_limit_exceeded"}}`),
			assertType: func(err error) bool {
				var target *GatewayRateLimitError
				return errors.As(err, &target)
			},
		},
		{
			name: "model not found",
			body: []byte(`{"error":{"message":"","type":"model_not_found","param":{"modelId":"gpt-5"}}}`),
			assertType: func(err error) bool {
				var target *GatewayModelNotFoundError
				return errors.As(err, &target)
			},
		},
		{
			name: "internal server error",
			body: []byte(`{"error":{"message":"","type":"internal_server_error"}}`),
			assertType: func(err error) bool {
				var target *GatewayInternalServerError
				return errors.As(err, &target)
			},
		},
		{
			name: "unknown type",
			body: []byte(`{"error":{"message":"","type":"new_error_type"}}`),
			assertType: func(err error) bool {
				var target *GatewayInternalServerError
				return errors.As(err, &target)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CreateGatewayErrorFromResponse(tt.body, 500, "fallback", nil, "api-key")
			if !tt.assertType(err) {
				t.Fatalf("unexpected error type %T", err)
			}
			if err.Error() != "" {
				t.Fatalf("Error() = %q, want empty string", err.Error())
			}
		})
	}
}

func TestCreateGatewayErrorFromResponseRejectsNonStringGenerationID(t *testing.T) {
	err := CreateGatewayErrorFromResponse(
		[]byte(`{"error":{"message":"rate","type":"rate_limit_exceeded"},"generationId":123}`),
		429,
		"fallback",
		nil,
		"api-key",
	)
	var responseErr *GatewayResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("error type = %T, want *GatewayResponseError", err)
	}
	if responseErr.ValidationError == nil {
		t.Fatal("expected validation error for non-string generationId")
	}
}

func TestCreateGatewayErrorFromResponseValidatesCodeField(t *testing.T) {
	validBodies := [][]byte{
		[]byte(`{"error":{"message":"bad","type":"invalid_request_error","code":"BAD_REQUEST"}}`),
		[]byte(`{"error":{"message":"bad","type":"invalid_request_error","code":400}}`),
		[]byte(`{"error":{"message":"bad","type":"invalid_request_error","code":null}}`),
		[]byte(`{"error":{"message":"bad","type":"invalid_request_error"}}`),
	}
	for _, body := range validBodies {
		err := CreateGatewayErrorFromResponse(body, 400, "fallback", nil, "api-key")
		var invalidErr *GatewayInvalidRequestError
		if !errors.As(err, &invalidErr) {
			t.Fatalf("body %s produced %T, want *GatewayInvalidRequestError", body, err)
		}
	}

	invalidBodies := [][]byte{
		[]byte(`{"error":{"message":"bad","type":"invalid_request_error","code":true}}`),
		[]byte(`{"error":{"message":"bad","type":"invalid_request_error","code":{"value":"BAD"}}}`),
		[]byte(`{"error":{"message":"bad","type":"invalid_request_error","code":["BAD"]}}`),
	}
	for _, body := range invalidBodies {
		err := CreateGatewayErrorFromResponse(body, 400, "fallback", nil, "api-key")
		var responseErr *GatewayResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("body %s produced %T, want *GatewayResponseError", body, err)
		}
		if responseErr.ValidationError == nil {
			t.Fatalf("body %s missing validation error", body)
		}
	}
}
