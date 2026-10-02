package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
)

// AuthResult is the outcome of Auth: AUTHORIZED means valid tokens are now
// stored (nothing further required of the caller); REDIRECT means the caller
// must complete redirectToAuthorization before Auth can be called again with
// the resulting authorization code. Matches TS AuthResult (oauth.ts).
type AuthResult string

const (
	AuthResultAuthorized AuthResult = "AUTHORIZED"
	AuthResultRedirect   AuthResult = "REDIRECT"
)

// AuthOptions configures a call to Auth, matching TS auth()'s options
// (oauth.ts). Set HasAuthorizationCode (and AuthorizationCode/CallbackState/
// CallbackIssuer) when completing an authorization-code callback; leave it
// false to check for/refresh existing tokens or start a new authorization.
type AuthOptions struct {
	ServerURL string

	HasAuthorizationCode bool
	AuthorizationCode    string
	CallbackState        string
	// CallbackIssuer is the `iss` parameter from the authorization response,
	// validated against the pinned issuer when present (hash 1f29230).
	CallbackIssuer string

	// Scope is an explicit scope override, typically extracted from a 401
	// challenge via ExtractWWWAuthenticateParams (hash 1011e33).
	Scope               string
	ResourceMetadataURL *url.URL

	HTTPClient *http.Client
}

// Auth drives one step of the OAuth 2.1 authorization_code + PKCE flow for an
// MCP server, matching TS auth() (oauth.ts). On invalid_client/
// unauthorized_client it retries once after invalidating credentials, but
// only when the current client information came from Dynamic Client
// Registration (hash 7fc2bc1: a pre-registered client is never silently
// replaced) and no authorization code is in flight. On invalid_grant it
// retries once after invalidating stored tokens.
func Auth(ctx context.Context, provider OAuthClientProvider, options AuthOptions) (AuthResult, error) {
	refreshAttempt := &oauthRefreshAttempt{}
	result, err := authInternal(ctx, provider, options, refreshAttempt)
	if err == nil {
		return result, nil
	}

	if IsInvalidClientError(err) || IsUnauthorizedClientError(err) {
		dynamic := false
		if reporter, ok := provider.(OAuthDynamicRegistrationReporter); ok {
			var reporterErr error
			dynamic, reporterErr = reporter.IsClientInformationDynamicallyRegistered(ctx)
			if reporterErr != nil {
				return "", reporterErr
			}
		}
		if options.HasAuthorizationCode || !dynamic {
			// Return the original invalid_client/unauthorized_client error
			// (not the reporter's own nil error) so the caller sees why
			// authorization failed.
			return "", err
		}
		if invalidator, ok := provider.(OAuthCredentialInvalidator); ok {
			if ierr := invalidator.InvalidateCredentials(ctx, OAuthInvalidateAll); ierr != nil {
				return "", ierr
			}
		}
		return authInternal(ctx, provider, options, nil)
	}

	if IsInvalidGrantError(err) {
		// If a refresh was attempted, identify exactly which (now stale)
		// tokens it tried so a provider sharing storage across clients can
		// atomically delete only that generation, preserving a concurrent
		// refresh's newer tokens (hash 4d0500e). Otherwise fall back to
		// unconditional invalidation, e.g. during an authorization-code
		// exchange.
		if ierr := invalidateOAuthTokens(ctx, provider, refreshAttempt.tokens); ierr != nil {
			return "", ierr
		}
		return authInternal(ctx, provider, options, nil)
	}

	return "", err
}

// oauthRefreshAttempt records the token generation a refresh attempt used,
// matching TS auth()'s closure-captured `refreshAttempt` object (hash
// 4d0500e). It lets Auth identify the stale tokens for conditional
// invalidation when the authorization server rejects the refresh with
// invalid_grant.
type oauthRefreshAttempt struct {
	tokens *OAuthTokens
}

// invalidateOAuthTokens invalidates stored OAuth tokens, matching TS
// provider.invalidateCredentials('tokens', context?). When tokens is
// non-nil and the provider implements OAuthTokenInvalidator, the specific
// token generation is passed through so providers sharing storage across
// clients can atomically compare-and-delete only that generation.
// Otherwise it falls back to the unconditional OAuthCredentialInvalidator
// form.
func invalidateOAuthTokens(ctx context.Context, provider OAuthClientProvider, tokens *OAuthTokens) error {
	if tokens != nil {
		if invalidator, ok := provider.(OAuthTokenInvalidator); ok {
			return invalidator.InvalidateCredentialsForTokens(ctx, *tokens)
		}
	}
	if invalidator, ok := provider.(OAuthCredentialInvalidator); ok {
		return invalidator.InvalidateCredentials(ctx, OAuthInvalidateTokens)
	}
	return nil
}

func authInternal(ctx context.Context, provider OAuthClientProvider, options AuthOptions, refreshAttempt *oauthRefreshAttempt) (AuthResult, error) {
	if options.ResourceMetadataURL != nil {
		if err := AssertOAuthResourceMetadataURLSameOrigin(options.ServerURL, options.ResourceMetadataURL.String()); err != nil {
			return "", err
		}
	}

	var resourceMetadata *OAuthProtectedResourceMetadata
	var authorizationServerURL string
	discoveryOpts := OAuthDiscoveryOptions{HTTPClient: options.HTTPClient}
	if options.ResourceMetadataURL != nil {
		discoveryOpts.ResourceMetadataURL = options.ResourceMetadataURL.String()
	}
	if metadata, err := DiscoverOAuthProtectedResourceMetadata(ctx, options.ServerURL, discoveryOpts); err == nil {
		resourceMetadata = &metadata
		if len(metadata.AuthorizationServers) > 0 {
			authorizationServerURL = metadata.AuthorizationServers[0]
		}
	}

	// A callback may not carry the original PRM URL from the authentication
	// challenge. Reuse the authorization server pinned before redirecting when
	// rediscovery does not select one (hash df91a09).
	var clientInformation *OAuthClientInformation
	var callbackASInfo *OAuthAuthorizationServerInformation
	if options.HasAuthorizationCode {
		info, err := provider.ClientInformation(ctx)
		if err != nil {
			return "", err
		}
		clientInformation = info
		if clientInformation != nil {
			asInfo, err := getStoredOAuthAuthorizationServerInformation(ctx, provider, clientInformation, nil)
			if err != nil {
				return "", err
			}
			callbackASInfo = asInfo
		}
	}

	if authorizationServerURL == "" {
		if callbackASInfo != nil {
			authorizationServerURL = callbackASInfo.AuthorizationServerURL
		} else {
			authorizationServerURL = options.ServerURL
		}
	}

	parsedServerURL, err := url.Parse(options.ServerURL)
	if err != nil {
		return "", err
	}
	parsedASURL, err := url.Parse(authorizationServerURL)
	if err != nil {
		return "", err
	}
	trustedASOrigin := ""
	if origin(parsedASURL) == origin(parsedServerURL) ||
		(isOAuthLoopbackHost(parsedServerURL.Hostname()) && isOAuthLoopbackHost(parsedASURL.Hostname())) {
		trustedASOrigin = origin(parsedASURL)
	}
	// An authorization server selected by response metadata is untrusted until
	// its target has passed the SSRF guard. A same-origin (or loopback-to-
	// loopback) server is already the developer-configured request target.
	if trustedASOrigin == "" {
		if err := assertSafeOAuthEndpoint(authorizationServerURL, false); err != nil {
			return "", err
		}
	}

	resource, err := SelectOAuthResourceURL(ctx, options.ServerURL, provider, resourceMetadata)
	if err != nil {
		return "", err
	}

	if validator, ok := provider.(OAuthAuthorizationServerURLValidator); ok {
		if err := validator.ValidateAuthorizationServerURL(ctx, options.ServerURL, authorizationServerURL); err != nil {
			return "", err
		}
	}

	metadata, err := DiscoverAuthorizationServerMetadata(ctx, authorizationServerURL, OAuthDiscoveryOptions{
		HTTPClient:    options.HTTPClient,
		TrustedOrigin: trustedASOrigin,
	})
	if err != nil {
		return "", err
	}
	currentASInfo, err := CreateOAuthAuthorizationServerInformation(authorizationServerURL, metadata)
	if err != nil {
		return "", err
	}

	clientMetadata := provider.ClientMetadata()
	selectedScope := SelectOAuthScope(options.Scope, resourceMetadata, clientMetadata)

	if !options.HasAuthorizationCode {
		info, err := provider.ClientInformation(ctx)
		if err != nil {
			return "", err
		}
		clientInformation = info
	}

	if clientInformation != nil && clientInformation.Issuer != "" {
		storedASInfo := callbackASInfo
		if storedASInfo == nil {
			storedASInfo, err = getStoredOAuthAuthorizationServerInformation(ctx, provider, clientInformation, nil)
			if err != nil {
				return "", err
			}
		}
		if storedASInfo != nil {
			if err := AssertOAuthAuthorizationServerInformationMatches(*storedASInfo, currentASInfo); err != nil {
				return "", err
			}
		}
	}

	if clientInformation == nil {
		if options.HasAuthorizationCode {
			return "", fmt.Errorf("existing OAuth client information is required when exchanging an authorization code")
		}
		saver, ok := provider.(OAuthClientInformationSaver)
		if !ok {
			return "", fmt.Errorf("OAuth client information must be saveable for dynamic registration")
		}

		registrationMetadata := clientMetadata
		registrationMetadata.Scope = selectedScope
		full, err := RegisterOAuthClient(ctx, authorizationServerURL, metadata, registrationMetadata, options.HTTPClient)
		if err != nil {
			return "", err
		}
		info := addOAuthASInfoToClientInformation(full.AsClientInformation(), currentASInfo)
		clientInformation = &info
		if err := saver.SaveClientInformation(ctx, *clientInformation); err != nil {
			return "", err
		}
	}

	// On callback, validate state and the AS pin before exchanging the code.
	if options.HasAuthorizationCode {
		if stateProvider, ok := provider.(OAuthStateProvider); ok {
			expected, hasExpected, err := stateProvider.StoredState(ctx)
			if err != nil {
				return "", err
			}
			if hasExpected {
				if err := ValidateOAuthState(options.CallbackState, expected); err != nil {
					return "", err
				}
			}
		}

		storedASInfo := callbackASInfo
		if storedASInfo == nil {
			storedASInfo, err = getStoredOAuthAuthorizationServerInformation(ctx, provider, clientInformation, nil)
			if err != nil {
				return "", err
			}
		}
		if storedASInfo == nil {
			return "", &MCPClientOAuthError{Message: "Stored OAuth authorization server metadata is required when exchanging an authorization code"}
		}

		expectedIssuer := storedASInfo.Issuer
		if expectedIssuer == "" && metadata != nil {
			expectedIssuer = metadata.Issuer
		}
		if expectedIssuer == "" {
			expectedIssuer = authorizationServerURL
		}
		if options.CallbackIssuer != "" && options.CallbackIssuer != expectedIssuer {
			return "", &MCPClientOAuthError{Message: fmt.Sprintf("OAuth authorization response issuer %s does not match expected issuer %s", options.CallbackIssuer, expectedIssuer)}
		}
		if err := AssertOAuthAuthorizationServerInformationMatches(*storedASInfo, currentASInfo); err != nil {
			return "", err
		}

		codeVerifier, err := provider.CodeVerifier(ctx)
		if err != nil {
			return "", err
		}

		tokens, err := ExchangeOAuthAuthorization(ctx, authorizationServerURL, ExchangeOAuthAuthorizationParams{
			Metadata:                metadata,
			ClientInformation:       *clientInformation,
			AuthorizationCode:       options.AuthorizationCode,
			CodeVerifier:            codeVerifier,
			RedirectURI:             provider.RedirectURL(),
			Resource:                resource,
			AddClientAuthentication: oauthAuthenticatorFunc(provider),
			HTTPClient:              options.HTTPClient,
		})
		if err != nil {
			return "", err
		}
		if err := provider.SaveTokens(ctx, addOAuthASInfoToTokens(*tokens, currentASInfo)); err != nil {
			return "", err
		}
		return AuthResultAuthorized, nil
	}

	tokens, err := provider.Tokens(ctx)
	if err != nil {
		return "", err
	}

	// Refresh only when stored credentials match the current AS pin.
	if tokens != nil && tokens.RefreshToken != "" {
		storedASInfo, err := getStoredOAuthAuthorizationServerInformation(ctx, provider, clientInformation, tokens)
		if err != nil {
			return "", err
		}

		if storedASInfo != nil {
			if err := AssertOAuthAuthorizationServerInformationMatches(*storedASInfo, currentASInfo); err != nil {
				return "", err
			}
		} else {
			tokensCopy := *tokens
			if err := invalidateOAuthTokens(ctx, provider, &tokensCopy); err != nil {
				return "", err
			}
		}

		if storedASInfo != nil {
			if refreshAttempt != nil {
				tokensCopy := *tokens
				refreshAttempt.tokens = &tokensCopy
			}
			newTokens, refreshErr := RefreshOAuthAuthorization(ctx, authorizationServerURL, RefreshOAuthAuthorizationParams{
				Metadata:                metadata,
				ClientInformation:       *clientInformation,
				RefreshToken:            tokens.RefreshToken,
				Resource:                resource,
				AddClientAuthentication: oauthAuthenticatorFunc(provider),
				HTTPClient:              options.HTTPClient,
			})
			if refreshErr == nil {
				if err := provider.SaveTokens(ctx, addOAuthASInfoToTokens(*newTokens, currentASInfo)); err != nil {
					return "", err
				}
				return AuthResultAuthorized, nil
			}

			// If this is a ServerError, or an unknown error type, swallow it
			// and continue to a fresh authorization flow below. Otherwise
			// (invalid_client/invalid_grant/unauthorized_client) escalate so
			// Auth's retry-once logic can react.
			var oauthErr *MCPClientOAuthError
			if errors.As(refreshErr, &oauthErr) && oauthErr.Code != OAuthErrorCodeServerError {
				return "", refreshErr
			}
		}
	}

	// Start a new authorization flow and persist the AS pin before redirecting.
	var state string
	if stateProvider, ok := provider.(OAuthStateProvider); ok {
		state, err = stateProvider.State(ctx)
		if err != nil {
			return "", err
		}
		if state != "" {
			if err := stateProvider.SaveState(ctx, state); err != nil {
				return "", err
			}
		}
	}

	authorizationURL, codeVerifier, err := StartOAuthAuthorization(authorizationServerURL, StartOAuthAuthorizationParams{
		Metadata:          metadata,
		ClientInformation: *clientInformation,
		RedirectURL:       provider.RedirectURL(),
		Scope:             selectedScope,
		State:             state,
		Resource:          resource,
	})
	if err != nil {
		return "", err
	}

	saved, err := saveOAuthAuthorizationServerInformation(ctx, provider, *clientInformation, currentASInfo)
	if err != nil {
		return "", err
	}
	if !saved {
		return "", &MCPClientOAuthError{Message: "OAuth authorization server metadata must be saveable before starting authorization"}
	}

	if err := provider.SaveCodeVerifier(ctx, codeVerifier); err != nil {
		return "", err
	}
	if err := provider.RedirectToAuthorization(ctx, authorizationURL); err != nil {
		return "", err
	}
	return AuthResultRedirect, nil
}

func oauthAuthenticatorFunc(provider OAuthClientProvider) func(context.Context, http.Header, url.Values, string, *OAuthAuthorizationServerMetadata) error {
	if authenticator, ok := provider.(OAuthClientAuthenticator); ok {
		return authenticator.AddClientAuthentication
	}
	return nil
}

// SelectOAuthResourceURL validates and selects the RFC 8707 resource value
// sent to the authorization server, matching TS selectResourceURL (oauth.ts).
// A nil result (with a nil error) means no resource parameter should be sent.
func SelectOAuthResourceURL(ctx context.Context, serverURL string, provider OAuthClientProvider, resourceMetadata *OAuthProtectedResourceMetadata) (*url.URL, error) {
	defaultResource, err := resourceURLFromServerURL(serverURL)
	if err != nil {
		return nil, err
	}

	if validator, ok := provider.(OAuthResourceURLValidator); ok {
		var configuredResource string
		if resourceMetadata != nil {
			configuredResource = resourceMetadata.Resource
		}
		selected, err := validator.ValidateResourceURL(ctx, defaultResource.String(), configuredResource)
		if err != nil {
			return nil, err
		}
		if selected == "" {
			return nil, nil
		}
		return url.Parse(selected)
	}

	if resourceMetadata == nil {
		return nil, nil
	}

	allowed, err := checkOAuthResourceAllowed(defaultResource.String(), resourceMetadata.Resource)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, fmt.Errorf("protected resource %s does not match expected %s (or origin)", resourceMetadata.Resource, defaultResource.String())
	}
	return url.Parse(resourceMetadata.Resource)
}

// SelectOAuthScope chooses the OAuth scope to request, in precedence order:
// an explicit challenge scope (from a 401's WWW-Authenticate header), then
// the protected resource metadata's scopes_supported, then the client's own
// configured scope. Matches TS selectScope (oauth.ts, hash 1011e33).
func SelectOAuthScope(challengeScope string, resourceMetadata *OAuthProtectedResourceMetadata, clientMetadata OAuthClientMetadata) string {
	if challengeScope != "" {
		return challengeScope
	}
	if resourceMetadata != nil && len(resourceMetadata.ScopesSupported) > 0 {
		return strings.Join(resourceMetadata.ScopesSupported, " ")
	}
	return clientMetadata.Scope
}

// inferOAuthApplicationType infers RFC 7591 application_type from a client's
// redirect URIs: "native" when every redirect URI is either a loopback
// http(s) URL or uses a non-http(s) (custom) scheme, "web" otherwise.
// Matches TS inferOAuthApplicationType (oauth.ts, hash 1f29230).
func inferOAuthApplicationType(redirectURIs []string) string {
	for _, raw := range redirectURIs {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "web"
		}
		isHTTPFamily := parsed.Scheme == "http" || parsed.Scheme == "https"
		isNative := (isHTTPFamily && isOAuthLoopbackHost(parsed.Hostname())) || !isHTTPFamily
		if !isNative {
			return "web"
		}
	}
	return "native"
}

type oauthClientAuthMethod string

const (
	oauthClientAuthBasic oauthClientAuthMethod = "client_secret_basic"
	oauthClientAuthPost  oauthClientAuthMethod = "client_secret_post"
	oauthClientAuthNone  oauthClientAuthMethod = "none"
)

// selectOAuthClientAuthMethod picks the best client authentication method
// given server support and client configuration, matching TS
// selectClientAuthMethod (oauth.ts): client_secret_basic > client_secret_post
// > none, preferring methods the client can actually satisfy.
func selectOAuthClientAuthMethod(clientInformation OAuthClientInformation, supportedMethods []string) oauthClientAuthMethod {
	hasSecret := clientInformation.ClientSecret != ""

	if len(supportedMethods) == 0 {
		if hasSecret {
			return oauthClientAuthPost
		}
		return oauthClientAuthNone
	}
	if hasSecret && containsOAuthString(supportedMethods, string(oauthClientAuthBasic)) {
		return oauthClientAuthBasic
	}
	if hasSecret && containsOAuthString(supportedMethods, string(oauthClientAuthPost)) {
		return oauthClientAuthPost
	}
	if containsOAuthString(supportedMethods, string(oauthClientAuthNone)) {
		return oauthClientAuthNone
	}
	if hasSecret {
		return oauthClientAuthPost
	}
	return oauthClientAuthNone
}

// applyOAuthClientAuthentication applies OAuth 2.1 client authentication
// (RFC 6749 §2.3.1 / §2.1), matching TS applyClientAuthentication (oauth.ts).
func applyOAuthClientAuthentication(method oauthClientAuthMethod, clientInformation OAuthClientInformation, headers http.Header, params url.Values) error {
	switch method {
	case oauthClientAuthBasic:
		if clientInformation.ClientSecret == "" {
			return fmt.Errorf("client_secret_basic authentication requires a client_secret")
		}
		credentials := base64.StdEncoding.EncodeToString([]byte(clientInformation.ClientID + ":" + clientInformation.ClientSecret))
		headers.Set("Authorization", "Basic "+credentials)
		return nil
	case oauthClientAuthPost:
		params.Set("client_id", clientInformation.ClientID)
		if clientInformation.ClientSecret != "" {
			params.Set("client_secret", clientInformation.ClientSecret)
		}
		return nil
	case oauthClientAuthNone:
		params.Set("client_id", clientInformation.ClientID)
		return nil
	default:
		return fmt.Errorf("unsupported client authentication method: %s", method)
	}
}

// StartOAuthAuthorizationParams configures StartOAuthAuthorization.
type StartOAuthAuthorizationParams struct {
	Metadata          *OAuthAuthorizationServerMetadata
	ClientInformation OAuthClientInformation
	RedirectURL       string
	Scope             string
	State             string
	Resource          *url.URL
}

// StartOAuthAuthorization builds the authorization URL and generates a fresh
// PKCE code_verifier for a new authorization_code flow, matching TS
// startAuthorization (oauth.ts). If scope contains "offline_access", a
// prompt=consent parameter is appended (required by some OIDC providers to
// guarantee a refresh token is issued).
func StartOAuthAuthorization(authorizationServerURL string, params StartOAuthAuthorizationParams) (authorizationURL string, codeVerifier string, err error) {
	const responseType = "code"
	const codeChallengeMethod = "S256"

	var authURL *url.URL
	if params.Metadata != nil {
		authURL, err = url.Parse(params.Metadata.AuthorizationEndpoint)
		if err != nil {
			return "", "", err
		}
		if !containsOAuthString(params.Metadata.ResponseTypesSupported, responseType) {
			return "", "", fmt.Errorf("incompatible auth server: does not support response type %s", responseType)
		}
		if len(params.Metadata.CodeChallengeMethodsSupported) == 0 || !containsOAuthString(params.Metadata.CodeChallengeMethodsSupported, codeChallengeMethod) {
			return "", "", fmt.Errorf("incompatible auth server: does not support code challenge method %s", codeChallengeMethod)
		}
	} else {
		base, parseErr := url.Parse(authorizationServerURL)
		if parseErr != nil {
			return "", "", parseErr
		}
		authURL = base.ResolveReference(&url.URL{Path: "/authorize"})
	}

	codeVerifier, codeChallenge, err := GenerateOAuthPKCE()
	if err != nil {
		return "", "", err
	}

	query := authURL.Query()
	query.Set("response_type", responseType)
	query.Set("client_id", params.ClientInformation.ClientID)
	query.Set("code_challenge", codeChallenge)
	query.Set("code_challenge_method", codeChallengeMethod)
	query.Set("redirect_uri", params.RedirectURL)
	if params.State != "" {
		query.Set("state", params.State)
	}
	if params.Scope != "" {
		query.Set("scope", params.Scope)
	}
	if strings.Contains(params.Scope, "offline_access") {
		// The request includes the OIDC-only "offline_access" scope: set
		// prompt=consent to ensure the user is prompted to grant offline
		// access. https://openid.net/specs/openid-connect-core-1_0.html#OfflineAccess
		query.Add("prompt", "consent")
	}
	if params.Resource != nil {
		query.Set("resource", resourceURLStripSlash(params.Resource))
	}
	authURL.RawQuery = query.Encode()

	return authURL.String(), codeVerifier, nil
}

// ExchangeOAuthAuthorizationParams configures ExchangeOAuthAuthorization.
type ExchangeOAuthAuthorizationParams struct {
	Metadata                *OAuthAuthorizationServerMetadata
	ClientInformation       OAuthClientInformation
	AuthorizationCode       string
	CodeVerifier            string
	RedirectURI             string
	Resource                *url.URL
	AddClientAuthentication func(ctx context.Context, headers http.Header, params url.Values, tokenURL string, metadata *OAuthAuthorizationServerMetadata) error
	HTTPClient              *http.Client
}

// ExchangeOAuthAuthorization exchanges an authorization code for tokens,
// matching TS exchangeAuthorization (oauth.ts). The token endpoint is
// guarded against SSRF (hash fe69342) and RFC 8707's resource parameter has
// its trailing slash stripped (hash 1e89d62).
func ExchangeOAuthAuthorization(ctx context.Context, authorizationServerURL string, params ExchangeOAuthAuthorizationParams) (*OAuthTokens, error) {
	const grantType = "authorization_code"

	tokenURL, err := oauthResolveTokenURL(authorizationServerURL, params.Metadata)
	if err != nil {
		return nil, err
	}
	allowLoopback := oauthEndpointAllowsLoopback(authorizationServerURL, tokenURL)
	if err := assertSafeOAuthEndpoint(tokenURL.String(), allowLoopback); err != nil {
		return nil, err
	}
	if params.Metadata != nil && len(params.Metadata.GrantTypesSupported) > 0 && !containsOAuthString(params.Metadata.GrantTypesSupported, grantType) {
		return nil, fmt.Errorf("incompatible auth server: does not support grant type %s", grantType)
	}

	headers := http.Header{}
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	headers.Set("Accept", "application/json")
	values := url.Values{}
	values.Set("grant_type", grantType)
	values.Set("code", params.AuthorizationCode)
	values.Set("code_verifier", params.CodeVerifier)
	values.Set("redirect_uri", params.RedirectURI)

	if err := applyOAuthTokenRequestAuthentication(ctx, params.AddClientAuthentication, params.ClientInformation, params.Metadata, headers, values, tokenURL.String()); err != nil {
		return nil, err
	}
	if params.Resource != nil {
		values.Set("resource", resourceURLStripSlash(params.Resource))
	}

	resp, err := doOAuthTokenPOST(ctx, params.HTTPClient, tokenURL.String(), allowLoopback, headers, []byte(values.Encode()))
	if err != nil {
		return nil, err
	}
	return decodeOAuthTokenResponse(resp)
}

// RefreshOAuthAuthorizationParams configures RefreshOAuthAuthorization.
type RefreshOAuthAuthorizationParams struct {
	Metadata                *OAuthAuthorizationServerMetadata
	ClientInformation       OAuthClientInformation
	RefreshToken            string
	Resource                *url.URL
	AddClientAuthentication func(ctx context.Context, headers http.Header, params url.Values, tokenURL string, metadata *OAuthAuthorizationServerMetadata) error
	HTTPClient              *http.Client
}

// RefreshOAuthAuthorization exchanges a refresh token for a new access token,
// matching TS refreshAuthorization (oauth.ts). If the server does not return
// a new refresh_token, the original one is preserved.
func RefreshOAuthAuthorization(ctx context.Context, authorizationServerURL string, params RefreshOAuthAuthorizationParams) (*OAuthTokens, error) {
	const grantType = "refresh_token"

	var tokenURL *url.URL
	var err error
	if params.Metadata != nil {
		tokenURL, err = url.Parse(params.Metadata.TokenEndpoint)
		if err != nil {
			return nil, err
		}
		if len(params.Metadata.GrantTypesSupported) > 0 && !containsOAuthString(params.Metadata.GrantTypesSupported, grantType) {
			return nil, fmt.Errorf("incompatible auth server: does not support grant type %s", grantType)
		}
	} else {
		base, parseErr := url.Parse(authorizationServerURL)
		if parseErr != nil {
			return nil, parseErr
		}
		tokenURL = base.ResolveReference(&url.URL{Path: "/token"})
	}
	allowLoopback := oauthEndpointAllowsLoopback(authorizationServerURL, tokenURL)
	if err := assertSafeOAuthEndpoint(tokenURL.String(), allowLoopback); err != nil {
		return nil, err
	}

	headers := http.Header{}
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	headers.Set("Accept", "application/json")
	values := url.Values{}
	values.Set("grant_type", grantType)
	values.Set("refresh_token", params.RefreshToken)

	if err := applyOAuthTokenRequestAuthentication(ctx, params.AddClientAuthentication, params.ClientInformation, params.Metadata, headers, values, tokenURL.String()); err != nil {
		return nil, err
	}
	if params.Resource != nil {
		values.Set("resource", resourceURLStripSlash(params.Resource))
	}

	resp, err := doOAuthTokenPOST(ctx, params.HTTPClient, tokenURL.String(), allowLoopback, headers, []byte(values.Encode()))
	if err != nil {
		return nil, err
	}
	tokens, err := decodeOAuthTokenResponse(resp)
	if err != nil {
		return nil, err
	}
	if tokens.RefreshToken == "" {
		tokens.RefreshToken = params.RefreshToken
	}
	return tokens, nil
}

// RegisterOAuthClient performs OAuth 2.0 Dynamic Client Registration (RFC
// 7591), matching TS registerClient (oauth.ts). The registration endpoint is
// guarded against SSRF (hash fe69342) and application_type is inferred when
// not explicitly set (hash 1f29230). Include the caller's selected scope in
// clientMetadata.Scope before calling (hash bf591f0).
func RegisterOAuthClient(ctx context.Context, authorizationServerURL string, metadata *OAuthAuthorizationServerMetadata, clientMetadata OAuthClientMetadata, httpClient *http.Client) (*OAuthClientInformationFull, error) {
	var registrationURL *url.URL
	var err error
	if metadata != nil {
		if metadata.RegistrationEndpoint == "" {
			return nil, fmt.Errorf("incompatible auth server: does not support dynamic client registration")
		}
		registrationURL, err = url.Parse(metadata.RegistrationEndpoint)
		if err != nil {
			return nil, err
		}
	} else {
		base, parseErr := url.Parse(authorizationServerURL)
		if parseErr != nil {
			return nil, parseErr
		}
		registrationURL = base.ResolveReference(&url.URL{Path: "/register"})
	}
	allowLoopback := oauthEndpointAllowsLoopback(authorizationServerURL, registrationURL)
	if err := assertSafeOAuthEndpoint(registrationURL.String(), allowLoopback); err != nil {
		return nil, err
	}

	if clientMetadata.ApplicationType == "" {
		clientMetadata.ApplicationType = inferOAuthApplicationType(clientMetadata.RedirectURIs)
	}

	body, err := json.Marshal(clientMetadata)
	if err != nil {
		return nil, err
	}

	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")

	resp, err := doOAuthTokenPOST(ctx, httpClient, registrationURL.String(), allowLoopback, headers, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ParseOAuthErrorResponse(resp)
	}
	defer resp.Body.Close() //nolint:errcheck
	rawBody, err := readLimitedOAuthBody(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to read dynamic client registration response: %w", err)
	}
	var full OAuthClientInformationFull
	if err := json.Unmarshal(rawBody, &full); err != nil {
		return nil, fmt.Errorf("failed to decode dynamic client registration response: %w", err)
	}
	return &full, nil
}

func applyOAuthTokenRequestAuthentication(
	ctx context.Context,
	addClientAuthentication func(context.Context, http.Header, url.Values, string, *OAuthAuthorizationServerMetadata) error,
	clientInformation OAuthClientInformation,
	metadata *OAuthAuthorizationServerMetadata,
	headers http.Header,
	values url.Values,
	tokenURL string,
) error {
	if addClientAuthentication != nil {
		return addClientAuthentication(ctx, headers, values, tokenURL, metadata)
	}
	var supported []string
	if metadata != nil {
		supported = metadata.TokenEndpointAuthMethods
	}
	method := selectOAuthClientAuthMethod(clientInformation, supported)
	return applyOAuthClientAuthentication(method, clientInformation, headers, values)
}

func oauthResolveTokenURL(authorizationServerURL string, metadata *OAuthAuthorizationServerMetadata) (*url.URL, error) {
	if metadata != nil && metadata.TokenEndpoint != "" {
		return url.Parse(metadata.TokenEndpoint)
	}
	base, err := url.Parse(authorizationServerURL)
	if err != nil {
		return nil, err
	}
	return base.ResolveReference(&url.URL{Path: "/token"}), nil
}

// trustedLoopbackOriginFor returns the authorization server's own origin when
// it is a loopback address (RFC 8252 §7.3), matching TS
// getTrustedLoopbackOrigin (oauth.ts).
func trustedLoopbackOriginFor(authorizationServerURL string) string {
	parsed, err := url.Parse(authorizationServerURL)
	if err != nil {
		return ""
	}
	if isOAuthLoopbackHost(parsed.Hostname()) {
		return origin(parsed)
	}
	return ""
}

// oauthEndpointAllowsLoopback reports whether endpoint shares the
// authorization server's loopback origin, matching TS's
// `allowLoopback: tokenUrl.origin === trustedOrigin` pattern.
func oauthEndpointAllowsLoopback(authorizationServerURL string, endpoint *url.URL) bool {
	trusted := trustedLoopbackOriginFor(authorizationServerURL)
	return trusted != "" && origin(endpoint) == trusted
}

// doOAuthTokenPOST issues a POST request to a token/registration endpoint,
// erroring on any redirect response (TS fetchWithValidatedEndpoint,
// redirect:'error') rather than following it — an authorization code, PKCE
// verifier, or client secret must never be replayed to a redirect target
// (hash fe69342).
func doOAuthTokenPOST(ctx context.Context, httpClient *http.Client, endpoint string, allowLoopback bool, headers http.Header, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header[k] = append([]string(nil), v...)
	}
	return oauthTokenPOSTClient(httpClient, allowLoopback).Do(req)
}

func oauthTokenPOSTClient(httpClient *http.Client, allowLoopback bool) *http.Client {
	var c http.Client
	if httpClient != nil {
		c = *httpClient
	} else if !allowLoopback {
		c.Transport = fileutil.SafeTransport()
	}
	c.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("mcp oauth: unexpected redirect to %s", req.URL)
	}
	return &c
}

func decodeOAuthTokenResponse(resp *http.Response) (*OAuthTokens, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ParseOAuthErrorResponse(resp)
	}
	defer resp.Body.Close() //nolint:errcheck
	rawBody, err := readLimitedOAuthBody(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to read OAuth token response: %w", err)
	}
	var tokens OAuthTokens
	if err := json.Unmarshal(rawBody, &tokens); err != nil {
		return nil, fmt.Errorf("failed to decode OAuth token response: %w", err)
	}
	return &tokens, nil
}

// getStoredOAuthAuthorizationServerInformation resolves the pinned AS info
// for existing credentials, preferring the tokens' own pin, then the
// provider's dedicated AS-info store, then the client information's pin.
// Matches TS getStoredAuthorizationServerInformation (oauth.ts).
func getStoredOAuthAuthorizationServerInformation(ctx context.Context, provider OAuthClientProvider, clientInformation *OAuthClientInformation, tokens *OAuthTokens) (*OAuthAuthorizationServerInformation, error) {
	if tokens != nil {
		if info := oauthASInfoFromCredentials(tokens.Issuer, tokens.AuthorizationServer, tokens.TokenEndpoint); info != nil {
			return info, nil
		}
	}
	if store, ok := provider.(OAuthAuthorizationServerInformationStore); ok {
		info, err := store.AuthorizationServerInformation(ctx)
		if err != nil {
			return nil, err
		}
		if info != nil {
			return &OAuthAuthorizationServerInformation{
				Issuer:                 info.Issuer,
				AuthorizationServerURL: normalizeOAuthURL(info.AuthorizationServerURL),
				TokenEndpoint:          normalizeOAuthURL(info.TokenEndpoint),
			}, nil
		}
	}
	if clientInformation != nil {
		if info := oauthASInfoFromCredentials(clientInformation.Issuer, clientInformation.AuthorizationServer, clientInformation.TokenEndpoint); info != nil {
			return info, nil
		}
	}
	return nil, nil
}

func oauthASInfoFromCredentials(issuer, authorizationServer, tokenEndpoint string) *OAuthAuthorizationServerInformation {
	if authorizationServer == "" || tokenEndpoint == "" {
		return nil
	}
	return &OAuthAuthorizationServerInformation{
		Issuer:                 issuer,
		AuthorizationServerURL: normalizeOAuthURL(authorizationServer),
		TokenEndpoint:          normalizeOAuthURL(tokenEndpoint),
	}
}

func addOAuthASInfoToTokens(tokens OAuthTokens, info OAuthAuthorizationServerInformation) OAuthTokens {
	tokens.Issuer = info.Issuer
	tokens.AuthorizationServer = info.AuthorizationServerURL
	tokens.TokenEndpoint = info.TokenEndpoint
	return tokens
}

func addOAuthASInfoToClientInformation(info OAuthClientInformation, asInfo OAuthAuthorizationServerInformation) OAuthClientInformation {
	info.Issuer = asInfo.Issuer
	info.AuthorizationServer = asInfo.AuthorizationServerURL
	info.TokenEndpoint = asInfo.TokenEndpoint
	return info
}

// saveOAuthAuthorizationServerInformation persists the AS pin, preferring a
// dedicated AS-info store and falling back to attaching it to client
// information. The bool result reports whether it was saved anywhere,
// matching TS saveAuthorizationServerInformation.
func saveOAuthAuthorizationServerInformation(ctx context.Context, provider OAuthClientProvider, clientInformation OAuthClientInformation, asInfo OAuthAuthorizationServerInformation) (bool, error) {
	if store, ok := provider.(OAuthAuthorizationServerInformationStore); ok {
		if err := store.SaveAuthorizationServerInformation(ctx, asInfo); err != nil {
			return false, err
		}
		return true, nil
	}
	if saver, ok := provider.(OAuthClientInformationSaver); ok {
		if err := saver.SaveClientInformation(ctx, addOAuthASInfoToClientInformation(clientInformation, asInfo)); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}
