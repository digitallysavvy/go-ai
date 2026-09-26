package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OAuth 2.0 error codes that Auth's retry-once logic special-cases, matching
// TS's OAUTH_ERRORS map (error/oauth-error.ts).
const (
	OAuthErrorCodeServerError        = "server_error"
	OAuthErrorCodeInvalidClient      = "invalid_client"
	OAuthErrorCodeInvalidGrant       = "invalid_grant"
	OAuthErrorCodeUnauthorizedClient = "unauthorized_client"
)

// MCPClientOAuthError is an error from the MCP OAuth flow. Code is the OAuth
// 2.0 error code (RFC 6749 §5.2) when known; for a response that could not be
// parsed as an OAuth error, Code is empty and Message carries a diagnostic
// message including the HTTP status and raw body, matching TS
// parseErrorResponse's ServerError fallback.
type MCPClientOAuthError struct {
	Code       string
	Message    string
	Cause      error
	StatusCode int
}

func (e *MCPClientOAuthError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code
	}
	return "MCP OAuth error"
}

func (e *MCPClientOAuthError) Unwrap() error {
	return e.Cause
}

// IsInvalidClientError reports whether err is an OAuth invalid_client error
// (TS InvalidClientError).
func IsInvalidClientError(err error) bool {
	return oauthErrorCodeIs(err, OAuthErrorCodeInvalidClient)
}

// IsInvalidGrantError reports whether err is an OAuth invalid_grant error
// (TS InvalidGrantError).
func IsInvalidGrantError(err error) bool {
	return oauthErrorCodeIs(err, OAuthErrorCodeInvalidGrant)
}

// IsUnauthorizedClientError reports whether err is an OAuth unauthorized_client
// error (TS UnauthorizedClientError).
func IsUnauthorizedClientError(err error) bool {
	return oauthErrorCodeIs(err, OAuthErrorCodeUnauthorizedClient)
}

func oauthErrorCodeIs(err error, code string) bool {
	oauthErr, ok := err.(*MCPClientOAuthError)
	return ok && oauthErr.Code == code
}

// oauthErrorResponseBody is the RFC 6749 §5.2 error response shape.
type oauthErrorResponseBody struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorURI         string `json:"error_uri,omitempty"`
}

// ParseOAuthErrorResponse parses an OAuth 2.0 error response body, matching
// TS parseErrorResponse (oauth.ts). If the body is a well-formed
// {error, error_description?, error_uri?} object, Code is set to the OAuth
// error code and Message to error_description (possibly empty). Otherwise a
// ServerError-equivalent (Code=OAuthErrorCodeServerError) is returned with a
// diagnostic Message that includes the HTTP status and raw body. resp.Body is
// consumed and closed.
func ParseOAuthErrorResponse(resp *http.Response) *MCPClientOAuthError {
	var body []byte
	if resp.Body != nil {
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close() //nolint:errcheck
	}

	var parsed oauthErrorResponseBody
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Error == "" {
		return &MCPClientOAuthError{
			Code:       OAuthErrorCodeServerError,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("HTTP %d: Invalid OAuth error response. Raw body: %s", resp.StatusCode, string(body)),
		}
	}

	var cause error
	if parsed.ErrorURI != "" {
		cause = fmt.Errorf("%s", parsed.ErrorURI)
	}
	return &MCPClientOAuthError{
		Code:       parsed.Error,
		StatusCode: resp.StatusCode,
		Message:    parsed.ErrorDescription,
		Cause:      cause,
	}
}
