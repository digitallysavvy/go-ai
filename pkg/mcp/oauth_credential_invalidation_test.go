package mcp

import (
	"context"
	"net/http"
	"sync"
	"testing"
)

// sharedOAuthTokenStore is OAuth token storage shared across multiple
// concurrentInvalidationOAuthProvider instances, modeling two clients
// (e.g. two server processes, or two in-flight requests) backed by the
// same credential store, as in TS's oauth-credential-invalidation.test.ts
// (hash 4d0500e).
type sharedOAuthTokenStore struct {
	mu     sync.Mutex
	tokens *OAuthTokens
}

// concurrentInvalidationOAuthProvider is a minimal OAuthClientProvider
// backed by a sharedOAuthTokenStore, implementing OAuthTokenInvalidator
// with atomic compare-and-delete semantics: InvalidateCredentialsForTokens
// only clears the store when it still holds exactly the generation being
// invalidated, so a concurrently saved (newer) generation survives.
type concurrentInvalidationOAuthProvider struct {
	store      *sharedOAuthTokenStore
	clientInfo *OAuthClientInformation

	mu                     sync.Mutex
	invalidateCalls        []OAuthCredentialInvalidationScope
	invalidateForTokens    []OAuthTokens
	deletedGenerationCount int
	redirectedTo           []string
}

func (p *concurrentInvalidationOAuthProvider) Tokens(ctx context.Context) (*OAuthTokens, error) {
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	return p.store.tokens, nil
}

func (p *concurrentInvalidationOAuthProvider) SaveTokens(ctx context.Context, tokens OAuthTokens) error {
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	p.store.tokens = &tokens
	return nil
}

func (p *concurrentInvalidationOAuthProvider) RedirectToAuthorization(ctx context.Context, authorizationURL string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.redirectedTo = append(p.redirectedTo, authorizationURL)
	return nil
}

func (p *concurrentInvalidationOAuthProvider) SaveCodeVerifier(ctx context.Context, codeVerifier string) error {
	return nil
}

func (p *concurrentInvalidationOAuthProvider) CodeVerifier(ctx context.Context) (string, error) {
	return "verifier", nil
}

func (p *concurrentInvalidationOAuthProvider) RedirectURL() string {
	return "https://app.example.com/callback"
}

func (p *concurrentInvalidationOAuthProvider) ClientMetadata() OAuthClientMetadata {
	return OAuthClientMetadata{RedirectURIs: []string{"https://app.example.com/callback"}}
}

func (p *concurrentInvalidationOAuthProvider) ClientInformation(ctx context.Context) (*OAuthClientInformation, error) {
	return p.clientInfo, nil
}

func (p *concurrentInvalidationOAuthProvider) SaveClientInformation(ctx context.Context, info OAuthClientInformation) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clientInfo = &info
	return nil
}

func (p *concurrentInvalidationOAuthProvider) InvalidateCredentials(ctx context.Context, scope OAuthCredentialInvalidationScope) error {
	p.mu.Lock()
	p.invalidateCalls = append(p.invalidateCalls, scope)
	p.mu.Unlock()
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	switch scope {
	case OAuthInvalidateAll, OAuthInvalidateTokens:
		p.store.tokens = nil
	}
	return nil
}

// InvalidateCredentialsForTokens implements OAuthTokenInvalidator: it only
// deletes the shared store's tokens when they are still exactly the
// generation being invalidated (compared by access+refresh token), so a
// concurrently saved newer generation is preserved. This is the behavior
// hash 4d0500e adds; a provider without it (or Auth without the fix) would
// instead call the unconditional InvalidateCredentials above and wipe out
// any newer tokens a concurrent refresh already saved.
func (p *concurrentInvalidationOAuthProvider) InvalidateCredentialsForTokens(ctx context.Context, tokens OAuthTokens) error {
	p.mu.Lock()
	p.invalidateForTokens = append(p.invalidateForTokens, tokens)
	p.mu.Unlock()

	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	if p.store.tokens != nil &&
		p.store.tokens.AccessToken == tokens.AccessToken &&
		p.store.tokens.RefreshToken == tokens.RefreshToken {
		p.store.tokens = nil
		p.mu.Lock()
		p.deletedGenerationCount++
		p.mu.Unlock()
	}
	return nil
}

var (
	_ OAuthClientProvider         = (*concurrentInvalidationOAuthProvider)(nil)
	_ OAuthClientInformationSaver = (*concurrentInvalidationOAuthProvider)(nil)
	_ OAuthCredentialInvalidator  = (*concurrentInvalidationOAuthProvider)(nil)
	_ OAuthTokenInvalidator       = (*concurrentInvalidationOAuthProvider)(nil)
)

// TestAuthPreservesConcurrentlySavedTokensOnStaleRefreshFailure mirrors TS
// oauth-credential-invalidation.test.ts's "snapshots the attempted tokens
// even when the provider mutates its token object" (hash 4d0500e). A
// refresh attempt is rejected with invalid_grant only after another client
// has already saved a newer token generation into the same shared store
// (simulating the loser of a concurrent refresh race). Without hash
// 4d0500e's conditional invalidation, Auth would unconditionally delete
// "tokens" on invalid_grant and destroy the concurrently saved winner; with
// it, the stale generation is identified and compared before deletion, so
// the winner survives and the retry succeeds using it.
func TestAuthPreservesConcurrentlySavedTokensOnStaleRefreshFailure(t *testing.T) {
	store := &sharedOAuthTokenStore{
		tokens: &OAuthTokens{
			AccessToken:         "old-access",
			RefreshToken:        "old-refresh",
			TokenType:           "Bearer",
			AuthorizationServer: "https://auth.example.com/",
			TokenEndpoint:       "https://auth.example.com/token",
		},
	}
	provider := &concurrentInvalidationOAuthProvider{
		store:      store,
		clientInfo: &OAuthClientInformation{ClientID: "existing-client"},
	}

	attempts := 0
	client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/token": func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				// A concurrent client wins the race and saves a newer
				// generation into the shared store before this (now
				// stale) refresh attempt's rejection is handled.
				store.mu.Lock()
				store.tokens = &OAuthTokens{
					AccessToken:         "new-access",
					RefreshToken:        "new-refresh",
					TokenType:           "Bearer",
					AuthorizationServer: "https://auth.example.com/",
					TokenEndpoint:       "https://auth.example.com/token",
				}
				store.mu.Unlock()
				return jsonResponse(http.StatusBadRequest, `{"error":"invalid_grant","error_description":"refresh token expired"}`), nil
			}
			return jsonResponse(http.StatusOK, `{"access_token":"rotated-access","token_type":"Bearer"}`), nil
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
		t.Fatalf("result = %s, want AUTHORIZED (retry should refresh using the preserved winner's tokens)", result)
	}

	provider.mu.Lock()
	invalidateForTokens := provider.invalidateForTokens
	deletedGenerationCount := provider.deletedGenerationCount
	unconditionalCalls := provider.invalidateCalls
	provider.mu.Unlock()

	if len(invalidateForTokens) != 1 || invalidateForTokens[0].AccessToken != "old-access" || invalidateForTokens[0].RefreshToken != "old-refresh" {
		t.Fatalf("invalidateForTokens = %#v, want exactly the stale snapshotted generation", invalidateForTokens)
	}
	if deletedGenerationCount != 0 {
		t.Fatalf("deletedGenerationCount = %d, want 0: the stale generation no longer matches the store, so nothing should be deleted", deletedGenerationCount)
	}
	if len(unconditionalCalls) != 0 {
		t.Fatalf("unconditional InvalidateCredentials calls = %v, want none: the context-aware path should have been used", unconditionalCalls)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.tokens == nil || store.tokens.RefreshToken != "new-refresh" {
		t.Fatalf("store.tokens = %#v, want the concurrently saved refresh token to have been reused for the successful retry", store.tokens)
	}
}

// TestAuthInvalidatesCurrentTokensWhenAuthorizationServerPinMissing mirrors
// TS's "includes the stored generation when invalidating tokens without an
// authorization server pin" (hash 4d0500e): when stored tokens carry no AS
// pin at all, Auth invalidates them (rather than attempting a refresh) and
// passes the specific generation through to OAuthTokenInvalidator.
func TestAuthInvalidatesCurrentTokensWhenAuthorizationServerPinMissing(t *testing.T) {
	store := &sharedOAuthTokenStore{
		tokens: &OAuthTokens{
			AccessToken:  "unpinned-access",
			RefreshToken: "unpinned-refresh",
			TokenType:    "Bearer",
			// No Issuer/AuthorizationServer/TokenEndpoint: this generation
			// was never pinned to an authorization server.
		},
	}
	provider := &concurrentInvalidationOAuthProvider{
		store:      store,
		clientInfo: &OAuthClientInformation{ClientID: "existing-client"},
	}

	client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/register": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusCreated, `{"client_id":"existing-client","redirect_uris":["https://app.example.com/callback"]}`), nil
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
		t.Fatalf("result = %s, want REDIRECT: an unpinned token generation must not be refreshed", result)
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.invalidateForTokens) != 1 || provider.invalidateForTokens[0].AccessToken != "unpinned-access" {
		t.Fatalf("invalidateForTokens = %#v, want the unpinned generation to have been identified", provider.invalidateForTokens)
	}
	if provider.deletedGenerationCount != 1 {
		t.Fatalf("deletedGenerationCount = %d, want 1: the current generation matches, so it should be deleted", provider.deletedGenerationCount)
	}
}
