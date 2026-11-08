package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// --- TS ae13fcea03: the resource is always sent ---

type r88toolsResourceValidator struct {
	*fakeOAuthProvider
	result string
	err    error

	gotDefault, gotConfigured string
}

func (p *r88toolsResourceValidator) ValidateResourceURL(_ context.Context, def, configured string) (string, error) {
	p.gotDefault, p.gotConfigured = def, configured
	return p.result, p.err
}

// oauth.test.ts "resource selection".
func TestR88toolsSelectResourceURLWithoutMetadata(t *testing.T) {
	ctx := context.Background()
	got, err := SelectOAuthResourceURL(ctx, "https://api.example.com/mcp-server?tenant=1#fragment", newFakeOAuthProvider(), nil)
	if err != nil || got == nil || got.String() != "https://api.example.com/mcp-server?tenant=1" {
		t.Fatalf("got %v, %v", got, err)
	}
	got, err = SelectOAuthResourceURL(ctx, "https://api.example.com", newFakeOAuthProvider(), nil)
	if err != nil || got == nil || resourceURLStripSlash(got) != "https://api.example.com" {
		t.Fatalf("got %v, %v", got, err)
	}

	for _, md := range []*OAuthProtectedResourceMetadata{nil, {Resource: "https://api.example.com"}} {
		p := &r88toolsResourceValidator{fakeOAuthProvider: newFakeOAuthProvider()}
		got, err := SelectOAuthResourceURL(ctx, "https://api.example.com/mcp-server?tenant=1#fragment", p, md)
		if err != nil || got == nil || got.String() != "https://api.example.com/mcp-server?tenant=1" {
			t.Fatalf("metadata %v: got %v, %v", md, got, err)
		}
		want := ""
		if md != nil {
			want = md.Resource
		}
		if p.gotDefault != "https://api.example.com/mcp-server?tenant=1" || p.gotConfigured != want {
			t.Fatalf("validator args %q / %q", p.gotDefault, p.gotConfigured)
		}
	}

	p := &r88toolsResourceValidator{fakeOAuthProvider: newFakeOAuthProvider(), result: "https://custom-resource.example.com/mcp"}
	got, err = SelectOAuthResourceURL(ctx, "https://api.example.com/mcp-server", p, nil)
	if err != nil || got.String() != "https://custom-resource.example.com/mcp" {
		t.Fatalf("custom: got %v, %v", got, err)
	}
	rejected := errors.New("Resource rejected")
	p = &r88toolsResourceValidator{fakeOAuthProvider: newFakeOAuthProvider(), err: rejected}
	if _, err := SelectOAuthResourceURL(ctx, "https://api.example.com/mcp-server", p, nil); err != rejected {
		t.Fatalf("err = %v, want the validator's error", err)
	}
}

const r88toolsASMetadataJSON = `{
	"issuer": "https://api.example.com/mcp-server",
	"authorization_endpoint": "https://api.example.com/authorize",
	"token_endpoint": "https://api.example.com/token",
	"registration_endpoint": "https://api.example.com/register",
	"response_types_supported": ["code"],
	"code_challenge_methods_supported": ["S256"],
	"grant_types_supported": ["authorization_code", "refresh_token"]
}`

type r88toolsCaptured struct {
	mu    sync.Mutex
	forms []url.Values
	posts int
}

func r88toolsNoPRMClient(t *testing.T, c *r88toolsCaptured) *http.Client {
	return oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound, `{}`), nil
		},
		"/.well-known/oauth-authorization-server": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, r88toolsASMetadataJSON), nil
		},
		"/register": func(req *http.Request) (*http.Response, error) {
			c.mu.Lock()
			c.posts++
			c.mu.Unlock()
			return jsonResponse(http.StatusCreated, `{"client_id":"dcr-client","redirect_uris":["https://app.example.com/callback"]}`), nil
		},
		"/token": func(req *http.Request) (*http.Response, error) {
			_ = req.ParseForm()
			c.mu.Lock()
			c.posts++
			c.forms = append(c.forms, req.PostForm)
			c.mu.Unlock()
			return jsonResponse(http.StatusOK, `{"access_token":"AT","refresh_token":"RT2","token_type":"Bearer"}`), nil
		},
	})
}

// oauth.test.ts "includes the canonical resource in authorization / token
// exchange / token refresh when Protected Resource Metadata is not present".
func TestR88toolsCanonicalResourceWithoutPRM(t *testing.T) {
	const serverURL = "https://api.example.com/mcp-server"
	asInfo := &OAuthAuthorizationServerInformation{
		Issuer: "https://api.example.com/mcp-server", AuthorizationServerURL: "https://api.example.com/mcp-server",
		TokenEndpoint: "https://api.example.com/token",
	}

	t.Run("authorization", func(t *testing.T) {
		c := &r88toolsCaptured{}
		p := newFakeOAuthProvider()
		p.clientInfo = &OAuthClientInformation{ClientID: "client"}
		p.asInfo = asInfo
		res, err := Auth(context.Background(), p, AuthOptions{ServerURL: serverURL, HTTPClient: r88toolsNoPRMClient(t, c)})
		if err != nil || res != AuthResultRedirect {
			t.Fatalf("Auth = %v, %v", res, err)
		}
		u, _ := url.Parse(p.redirectedTo[0])
		if got := u.Query().Get("resource"); got != serverURL {
			t.Fatalf("authorization resource = %q", got)
		}
	})

	t.Run("exchange", func(t *testing.T) {
		c := &r88toolsCaptured{}
		p := newFakeOAuthProvider()
		p.clientInfo = &OAuthClientInformation{ClientID: "client"}
		p.asInfo = asInfo
		p.codeVerifier = "verifier"
		res, err := Auth(context.Background(), p, AuthOptions{
			ServerURL: serverURL, HTTPClient: r88toolsNoPRMClient(t, c),
			HasAuthorizationCode: true, AuthorizationCode: "auth-code-123",
		})
		if err != nil || res != AuthResultAuthorized {
			t.Fatalf("Auth = %v, %v", res, err)
		}
		if len(c.forms) != 1 || c.forms[0].Get("resource") != serverURL || c.forms[0].Get("code") != "auth-code-123" {
			t.Fatalf("token forms = %v", c.forms)
		}
	})

	t.Run("refresh", func(t *testing.T) {
		c := &r88toolsCaptured{}
		p := newFakeOAuthProvider()
		p.clientInfo = &OAuthClientInformation{ClientID: "client"}
		p.asInfo = asInfo
		p.tokens = &OAuthTokens{AccessToken: "old", RefreshToken: "refresh123"}
		res, err := Auth(context.Background(), p, AuthOptions{ServerURL: serverURL, HTTPClient: r88toolsNoPRMClient(t, c)})
		if err != nil || res != AuthResultAuthorized {
			t.Fatalf("Auth = %v, %v", res, err)
		}
		f := c.forms[0]
		if f.Get("resource") != serverURL || f.Get("grant_type") != "refresh_token" || f.Get("refresh_token") != "refresh123" {
			t.Fatalf("refresh form = %v", f)
		}
	})
}

// --- TS dc0f7f243d: an unbound refresh token never takes a new binding ---

// oauth.test.ts "does not authenticate an unbound stored token using a new
// registration".
func TestR88toolsUnboundTokenClearedBeforeRegistration(t *testing.T) {
	c := &r88toolsCaptured{}
	p := newFakeOAuthProvider()
	p.tokens = &OAuthTokens{AccessToken: "test-access", RefreshToken: "test-refresh", TokenType: "Bearer"}
	res, err := Auth(context.Background(), p, AuthOptions{ServerURL: "https://api.example.com/mcp-server", HTTPClient: r88toolsNoPRMClient(t, c)})
	if err != nil || res != AuthResultRedirect {
		t.Fatalf("Auth = %v, %v", res, err)
	}
	if len(c.forms) != 0 {
		t.Fatalf("the unbound token reached the token endpoint: %v", c.forms)
	}
	if len(p.invalidatedLog) != 1 || p.invalidatedLog[0] != OAuthInvalidateTokens {
		t.Fatalf("invalidations = %v", p.invalidatedLog)
	}
	if len(p.savedClientInfo) != 1 || len(p.savedTokens) != 0 || len(p.redirectedTo) != 1 {
		t.Fatalf("saved client %d, saved tokens %d, redirects %d", len(p.savedClientInfo), len(p.savedTokens), len(p.redirectedTo))
	}
}

// A provider without any invalidation hook.
type r88toolsNoInvalidator struct{ OAuthClientProvider }

// A provider whose invalidation leaves the tokens in storage.
type r88toolsIneffectiveInvalidator struct{ *fakeOAuthProvider }

func (p *r88toolsIneffectiveInvalidator) InvalidateCredentials(context.Context, OAuthCredentialInvalidationScope) error {
	return nil
}

// oauth.test.ts "does not save a new binding when token invalidation is
// missing / ineffective".
func TestR88toolsUnboundTokenThatCannotBeCleared(t *testing.T) {
	for name, wrap := range map[string]func(*fakeOAuthProvider) OAuthClientProvider{
		"missing":     func(f *fakeOAuthProvider) OAuthClientProvider { return r88toolsNoInvalidator{f} },
		"ineffective": func(f *fakeOAuthProvider) OAuthClientProvider { return &r88toolsIneffectiveInvalidator{f} },
	} {
		t.Run(name, func(t *testing.T) {
			c := &r88toolsCaptured{}
			f := newFakeOAuthProvider()
			f.tokens = &OAuthTokens{AccessToken: "test-access", RefreshToken: "test-refresh", TokenType: "Bearer"}
			_, err := Auth(context.Background(), wrap(f), AuthOptions{ServerURL: "https://api.example.com/mcp-server", HTTPClient: r88toolsNoPRMClient(t, c)})
			var oauthErr *MCPClientOAuthError
			if !errors.As(err, &oauthErr) || oauthErr.Message != "Stored OAuth tokens without an authorization server binding must be cleared before starting authorization" {
				t.Fatalf("err = %v", err)
			}
			if len(f.savedClientInfo) != 0 || f.asInfo != nil || len(f.redirectedTo) != 0 || c.posts != 0 {
				t.Fatalf("saved client %d, AS info %v, redirects %d, POSTs %d", len(f.savedClientInfo), f.asInfo, len(f.redirectedTo), c.posts)
			}
		})
	}
}

// --- TS f608c056a2 (mcp half): HTTPS to HTTP discovery redirects ---

// oauth.test.ts "rejects protected resource / authorization server
// discovery redirects to $location".
func TestR88toolsDiscoveryRejectsDowngradeRedirects(t *testing.T) {
	for _, tc := range []struct {
		name, location, want string
	}{
		{"prm private", "https://169.254.169.254/latest/meta-data", "169.254.169.254"},
		{"prm downgrade", "http://resource.example.com/.well-known/oauth-protected-resource", "Redirect from HTTPS to HTTP was blocked"},
		{"as private", "https://127.0.0.1:4000/.well-known/openid-configuration", "127.0.0.1"},
		{"as downgrade", "http://auth.example.com/.well-known/oauth-authorization-server", "Redirect from HTTPS to HTTP was blocked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			calls := 0
			client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
				mu.Lock()
				calls++
				mu.Unlock()
				resp := jsonResponse(http.StatusFound, ``)
				resp.Header.Set("Location", tc.location)
				return resp, nil
			})
			var err error
			if strings.HasPrefix(tc.name, "prm") {
				_, err = DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://resource.example.com/mcp", OAuthDiscoveryOptions{HTTPClient: client})
			} else {
				_, err = DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com", OAuthDiscoveryOptions{HTTPClient: client})
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if calls != 1 {
				t.Fatalf("fetch calls = %d, want 1", calls)
			}
		})
	}
}
