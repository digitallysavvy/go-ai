package errors

import (
	"errors"
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

	err = CreateGatewayErrorFromResponse(
		[]byte(`{"error":{"message":"unknown","type":"new_type","param":null}}`),
		500,
		"default",
		cause,
		"api-key",
	)
	var internalErr *GatewayInternalServerError
	if !errors.As(err, &internalErr) {
		t.Fatalf("error type = %T, want *GatewayInternalServerError", err)
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
