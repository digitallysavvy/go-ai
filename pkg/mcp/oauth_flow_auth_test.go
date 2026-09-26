package mcp

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// fakeOAuthProvider is a full in-memory OAuthClientProvider (plus every
// optional capability interface) used to exercise Auth end to end, mirroring
// the reference implementation used by TS's oauth.test.ts.
type fakeOAuthProvider struct {
	mu sync.Mutex

	redirectURL    string
	clientMetadata OAuthClientMetadata

	tokens       *OAuthTokens
	clientInfo   *OAuthClientInformation
	codeVerifier string
	asInfo       *OAuthAuthorizationServerInformation

	dynamicallyRegistered bool
	state                 string
	storedState           *string

	redirectedTo    []string
	invalidatedLog  []OAuthCredentialInvalidationScope
	savedClientInfo []OAuthClientInformation
	savedTokens     []OAuthTokens
}

func newFakeOAuthProvider() *fakeOAuthProvider {
	return &fakeOAuthProvider{
		redirectURL:    "https://app.example.com/callback",
		clientMetadata: OAuthClientMetadata{RedirectURIs: []string{"https://app.example.com/callback"}},
	}
}

func (p *fakeOAuthProvider) Tokens(ctx context.Context) (*OAuthTokens, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tokens, nil
}

func (p *fakeOAuthProvider) SaveTokens(ctx context.Context, tokens OAuthTokens) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tokens = &tokens
	p.savedTokens = append(p.savedTokens, tokens)
	return nil
}

func (p *fakeOAuthProvider) RedirectToAuthorization(ctx context.Context, authorizationURL string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.redirectedTo = append(p.redirectedTo, authorizationURL)
	return nil
}

func (p *fakeOAuthProvider) SaveCodeVerifier(ctx context.Context, codeVerifier string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.codeVerifier = codeVerifier
	return nil
}

func (p *fakeOAuthProvider) CodeVerifier(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.codeVerifier, nil
}

func (p *fakeOAuthProvider) RedirectURL() string { return p.redirectURL }

func (p *fakeOAuthProvider) ClientMetadata() OAuthClientMetadata { return p.clientMetadata }

func (p *fakeOAuthProvider) ClientInformation(ctx context.Context) (*OAuthClientInformation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.clientInfo, nil
}

func (p *fakeOAuthProvider) SaveClientInformation(ctx context.Context, info OAuthClientInformation) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clientInfo = &info
	p.savedClientInfo = append(p.savedClientInfo, info)
	return nil
}

func (p *fakeOAuthProvider) IsClientInformationDynamicallyRegistered(ctx context.Context) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dynamicallyRegistered, nil
}

func (p *fakeOAuthProvider) InvalidateCredentials(ctx context.Context, scope OAuthCredentialInvalidationScope) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.invalidatedLog = append(p.invalidatedLog, scope)
	switch scope {
	case OAuthInvalidateAll:
		p.clientInfo = nil
		p.tokens = nil
		p.codeVerifier = ""
	case OAuthInvalidateClient:
		p.clientInfo = nil
	case OAuthInvalidateTokens:
		p.tokens = nil
	case OAuthInvalidateVerifier:
		p.codeVerifier = ""
	}
	return nil
}

func (p *fakeOAuthProvider) AuthorizationServerInformation(ctx context.Context) (*OAuthAuthorizationServerInformation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.asInfo, nil
}

func (p *fakeOAuthProvider) SaveAuthorizationServerInformation(ctx context.Context, info OAuthAuthorizationServerInformation) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asInfo = &info
	return nil
}

func (p *fakeOAuthProvider) State(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state, nil
}

func (p *fakeOAuthProvider) SaveState(ctx context.Context, state string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.storedState = &state
	return nil
}

func (p *fakeOAuthProvider) StoredState(ctx context.Context) (string, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.storedState == nil {
		return "", false, nil
	}
	return *p.storedState, true, nil
}

var (
	_ OAuthClientProvider                      = (*fakeOAuthProvider)(nil)
	_ OAuthDynamicRegistrationReporter         = (*fakeOAuthProvider)(nil)
	_ OAuthCredentialInvalidator               = (*fakeOAuthProvider)(nil)
	_ OAuthClientInformationSaver              = (*fakeOAuthProvider)(nil)
	_ OAuthAuthorizationServerInformationStore = (*fakeOAuthProvider)(nil)
	_ OAuthStateProvider                       = (*fakeOAuthProvider)(nil)
)

// oauthFlowTestServer routes discovery/registration/token requests for the
// Auth() integration tests. Routes are matched by substring against the
// request path (rather than exact match) so the test is not coupled to the
// exact RFC 8414/9728 path-aware discovery candidate ordering: any
// unmatched candidate URL (e.g. a path-aware probe the handler doesn't care
// about) falls through to a generic 404, which the discovery loop treats as
// "try the next candidate".
func oauthFlowTestServer(t *testing.T, routes map[string]func(req *http.Request) (*http.Response, error)) *http.Client {
	t.Helper()
	return oauthTestClient(func(req *http.Request) (*http.Response, error) {
		for substr, handler := range routes {
			if strings.Contains(req.URL.Path, substr) {
				return handler(req)
			}
		}
		return jsonResponse(http.StatusNotFound, `{"error":"not_found"}`), nil
	})
}

const oauthFlowASMetadataJSON = `{
	"issuer": "https://auth.example.com",
	"authorization_endpoint": "https://auth.example.com/authorize",
	"token_endpoint": "https://auth.example.com/token",
	"registration_endpoint": "https://auth.example.com/register",
	"response_types_supported": ["code"],
	"code_challenge_methods_supported": ["S256"],
	"grant_types_supported": ["authorization_code", "refresh_token"]
}`

// TestAuthDynamicRegistrationThenRedirect mirrors TS auth()'s first-time
// flow: no stored client -> DCR -> new authorization -> REDIRECT.
func TestAuthDynamicRegistrationThenRedirect(t *testing.T) {
	provider := newFakeOAuthProvider()
	client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/register": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusCreated, `{"client_id":"dcr-client","redirect_uris":["https://app.example.com/callback"]}`), nil
		},
	})

	result, err := Auth(context.Background(), provider, AuthOptions{
		ServerURL:  "https://auth.example.com/mcp",
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("Auth error: %v", err)
	}
	if result != AuthResultRedirect {
		t.Fatalf("result = %s, want REDIRECT", result)
	}
	if len(provider.redirectedTo) != 1 {
		t.Fatalf("redirectedTo = %v", provider.redirectedTo)
	}
	if !strings.HasPrefix(provider.redirectedTo[0], "https://auth.example.com/authorize?") {
		t.Fatalf("authorization URL = %s", provider.redirectedTo[0])
	}
	if provider.clientInfo == nil || provider.clientInfo.ClientID != "dcr-client" {
		t.Fatalf("clientInfo = %#v, want DCR-issued client saved", provider.clientInfo)
	}
	if provider.codeVerifier == "" {
		t.Fatal("expected a PKCE code verifier to be saved")
	}
	if provider.asInfo == nil || provider.asInfo.TokenEndpoint != "https://auth.example.com/token" {
		t.Fatalf("asInfo = %#v", provider.asInfo)
	}
}

// TestAuthExchangesAuthorizationCode mirrors TS auth()'s callback flow.
func TestAuthExchangesAuthorizationCode(t *testing.T) {
	provider := newFakeOAuthProvider()
	provider.clientInfo = &OAuthClientInformation{ClientID: "existing-client"}
	provider.codeVerifier = "verifier-xyz"
	provider.storedState = strPtr("expected-state")
	// Simulates the AS pin persisted by a prior Auth() call that returned
	// REDIRECT: the callback exchange requires this pin to already exist.
	provider.asInfo = &OAuthAuthorizationServerInformation{
		Issuer:                 "https://auth.example.com",
		AuthorizationServerURL: "https://auth.example.com/",
		TokenEndpoint:          "https://auth.example.com/token",
	}

	client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/token": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"access_token":"AT","token_type":"Bearer","refresh_token":"RT"}`), nil
		},
	})

	result, err := Auth(context.Background(), provider, AuthOptions{
		ServerURL:            "https://auth.example.com/mcp",
		HasAuthorizationCode: true,
		AuthorizationCode:    "the-code",
		CallbackState:        "expected-state",
		HTTPClient:           client,
	})
	if err != nil {
		t.Fatalf("Auth error: %v", err)
	}
	if result != AuthResultAuthorized {
		t.Fatalf("result = %s, want AUTHORIZED", result)
	}
	if provider.tokens == nil || provider.tokens.AccessToken != "AT" {
		t.Fatalf("tokens = %#v", provider.tokens)
	}
	if provider.tokens.TokenEndpoint != "https://auth.example.com/token" {
		t.Fatalf("expected AS pin attached to saved tokens: %#v", provider.tokens)
	}
}

func TestAuthExchangeRejectsStateMismatch(t *testing.T) {
	provider := newFakeOAuthProvider()
	provider.clientInfo = &OAuthClientInformation{ClientID: "existing-client"}
	provider.codeVerifier = "verifier-xyz"
	provider.storedState = strPtr("expected-state")

	client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
	})

	_, err := Auth(context.Background(), provider, AuthOptions{
		ServerURL:            "https://auth.example.com/mcp",
		HasAuthorizationCode: true,
		AuthorizationCode:    "the-code",
		CallbackState:        "wrong-state",
		HTTPClient:           client,
	})
	if err == nil || !strings.Contains(err.Error(), "CSRF") {
		t.Fatalf("err = %v, want CSRF state mismatch error", err)
	}
}

// TestAuthRefreshesExistingTokens mirrors TS auth()'s refresh path.
func TestAuthRefreshesExistingTokens(t *testing.T) {
	provider := newFakeOAuthProvider()
	provider.clientInfo = &OAuthClientInformation{ClientID: "existing-client"}
	provider.tokens = &OAuthTokens{
		AccessToken:         "OLD_AT",
		RefreshToken:        "RT",
		Issuer:              "https://auth.example.com",
		AuthorizationServer: "https://auth.example.com/",
		TokenEndpoint:       "https://auth.example.com/token",
	}

	client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/token": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"access_token":"NEW_AT","token_type":"Bearer"}`), nil
		},
	})

	result, err := Auth(context.Background(), provider, AuthOptions{
		ServerURL:  "https://auth.example.com/mcp",
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("Auth error: %v", err)
	}
	if result != AuthResultAuthorized {
		t.Fatalf("result = %s, want AUTHORIZED", result)
	}
	if provider.tokens.AccessToken != "NEW_AT" || provider.tokens.RefreshToken != "RT" {
		t.Fatalf("tokens after refresh = %#v", provider.tokens)
	}
}

// TestAuthRetriesOnceForDynamicClientAfterInvalidClient mirrors TS auth()'s
// retry-once semantics (hash 7fc2bc1): a dynamically registered client is
// invalidated and re-registered after invalid_client, then the flow
// succeeds.
func TestAuthRetriesOnceForDynamicClientAfterInvalidClient(t *testing.T) {
	provider := newFakeOAuthProvider()
	provider.clientInfo = &OAuthClientInformation{ClientID: "stale-dcr-client"}
	provider.dynamicallyRegistered = true
	provider.tokens = &OAuthTokens{
		AccessToken:         "OLD_AT",
		RefreshToken:        "RT",
		AuthorizationServer: "https://auth.example.com/",
		TokenEndpoint:       "https://auth.example.com/token",
	}

	var tokenAttempts int
	client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/register": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusCreated, `{"client_id":"fresh-dcr-client","redirect_uris":["https://app.example.com/callback"]}`), nil
		},
		"/token": func(req *http.Request) (*http.Response, error) {
			tokenAttempts++
			if tokenAttempts == 1 {
				return jsonResponse(http.StatusBadRequest, `{"error":"invalid_client","error_description":"unknown client"}`), nil
			}
			return jsonResponse(http.StatusOK, `{"access_token":"NEW_AT","token_type":"Bearer"}`), nil
		},
	})

	result, err := Auth(context.Background(), provider, AuthOptions{
		ServerURL:  "https://auth.example.com/mcp",
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("Auth error: %v", err)
	}
	if result != AuthResultRedirect {
		// After invalidating "all" credentials (client+tokens+verifier), the
		// retry has no refresh token left, so it re-registers and starts a
		// fresh authorization (REDIRECT), matching TS: invalidateCredentials
		// clears tokens too under scope "all".
		t.Fatalf("result = %s, want REDIRECT after full credential invalidation and re-registration", result)
	}
	if len(provider.invalidatedLog) != 1 || provider.invalidatedLog[0] != OAuthInvalidateAll {
		t.Fatalf("invalidatedLog = %v, want a single 'all' invalidation", provider.invalidatedLog)
	}
	if provider.clientInfo == nil || provider.clientInfo.ClientID != "fresh-dcr-client" {
		t.Fatalf("clientInfo after retry = %#v, want freshly re-registered client", provider.clientInfo)
	}
}

// TestAuthDoesNotRetryForPreRegisteredClientAfterInvalidClient mirrors TS
// auth()'s provenance guard (hash 7fc2bc1): a pre-registered (non-dynamic)
// client is never silently invalidated/replaced.
func TestAuthDoesNotRetryForPreRegisteredClientAfterInvalidClient(t *testing.T) {
	provider := newFakeOAuthProvider()
	provider.clientInfo = &OAuthClientInformation{ClientID: "pre-registered-client"}
	provider.dynamicallyRegistered = false
	provider.tokens = &OAuthTokens{
		AccessToken:         "OLD_AT",
		RefreshToken:        "RT",
		AuthorizationServer: "https://auth.example.com/",
		TokenEndpoint:       "https://auth.example.com/token",
	}

	client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/token": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusBadRequest, `{"error":"invalid_client","error_description":"unknown client"}`), nil
		},
	})

	_, err := Auth(context.Background(), provider, AuthOptions{
		ServerURL:  "https://auth.example.com/mcp",
		HTTPClient: client,
	})
	if err == nil || !IsInvalidClientError(err) {
		t.Fatalf("err = %v, want the raw invalid_client error to propagate", err)
	}
	if len(provider.invalidatedLog) != 0 {
		t.Fatalf("invalidatedLog = %v, want no invalidation for a pre-registered client", provider.invalidatedLog)
	}
	if provider.clientInfo.ClientID != "pre-registered-client" {
		t.Fatalf("clientInfo must be untouched: %#v", provider.clientInfo)
	}
}

// TestAuthRetriesOnceAfterInvalidGrant mirrors TS auth()'s invalid_grant
// retry-once semantics: stored tokens are invalidated and a fresh
// authorization flow starts.
func TestAuthRetriesOnceAfterInvalidGrant(t *testing.T) {
	provider := newFakeOAuthProvider()
	provider.clientInfo = &OAuthClientInformation{ClientID: "existing-client"}
	provider.tokens = &OAuthTokens{
		AccessToken:         "OLD_AT",
		RefreshToken:        "RT",
		AuthorizationServer: "https://auth.example.com/",
		TokenEndpoint:       "https://auth.example.com/token",
	}

	client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/token": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusBadRequest, `{"error":"invalid_grant","error_description":"refresh token expired"}`), nil
		},
	})

	result, err := Auth(context.Background(), provider, AuthOptions{
		ServerURL:  "https://auth.example.com/mcp",
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("Auth error: %v", err)
	}
	if result != AuthResultRedirect {
		t.Fatalf("result = %s, want REDIRECT after tokens are invalidated", result)
	}
	if len(provider.invalidatedLog) != 1 || provider.invalidatedLog[0] != OAuthInvalidateTokens {
		t.Fatalf("invalidatedLog = %v, want a single 'tokens' invalidation", provider.invalidatedLog)
	}
	if provider.tokens != nil {
		t.Fatalf("tokens should have been cleared: %#v", provider.tokens)
	}
}

func strPtr(s string) *string { return &s }
