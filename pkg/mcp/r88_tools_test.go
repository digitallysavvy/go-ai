package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
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
