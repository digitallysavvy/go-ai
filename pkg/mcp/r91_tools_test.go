package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TS 88b95757c1 oauth-discovery-runtime.test.ts "%s endpoints": registration,
// code exchange and refresh accept a developer-configured trustedOrigin.
// An endpoint on that origin is allowed (here a loopback one, which the SSRF
// guard otherwise rejects for a non-loopback authorization server) and an
// endpoint off it is still guarded.
func TestR91toolsOAuthEndpointsTrustedOrigin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"client_id":"example-client","redirect_uris":["https://app.example.com/callback"],"access_token":"example-token","token_type":"Bearer"}`))
	}))
	t.Cleanup(srv.Close)
	const issuer = "https://auth.example.com"
	metadata := &OAuthAuthorizationServerMetadata{
		Issuer: issuer, AuthorizationEndpoint: issuer + "/authorize",
		TokenEndpoint: srv.URL + "/token", RegistrationEndpoint: srv.URL + "/register",
		ResponseTypesSupported: []string{"code"},
	}
	client := OAuthClientInformation{ClientID: "example-client"}
	ops := map[string]func(trusted string) error{
		"registration": func(trusted string) error {
			_, err := RegisterOAuthClientWithOptions(context.Background(), issuer, RegisterOAuthClientOptions{
				Metadata: metadata, ClientMetadata: OAuthClientMetadata{RedirectURIs: []string{"https://app.example.com/callback"}}, TrustedOrigin: trusted,
			})
			return err
		},
		"code exchange": func(trusted string) error {
			_, err := ExchangeOAuthAuthorization(context.Background(), issuer, ExchangeOAuthAuthorizationParams{
				Metadata: metadata, ClientInformation: client, AuthorizationCode: "unused", CodeVerifier: "unused",
				RedirectURI: "https://app.example.com/callback", TrustedOrigin: trusted,
			})
			return err
		},
		"refresh": func(trusted string) error {
			_, err := RefreshOAuthAuthorization(context.Background(), issuer, RefreshOAuthAuthorizationParams{
				Metadata: metadata, ClientInformation: client, RefreshToken: "unused", TrustedOrigin: trusted,
			})
			return err
		},
	}
	for name, op := range ops {
		if err := op(""); err == nil || !strings.Contains(err.Error(), "OAuth endpoint URL is not allowed") {
			t.Errorf("%s without trustedOrigin: err = %v, want the SSRF guard", name, err)
		}
		if err := op(srv.URL); err != nil {
			t.Errorf("%s with trustedOrigin: %v", name, err)
		}
		if err := op("https://other.example.com"); err == nil {
			t.Errorf("%s with another trustedOrigin: want the SSRF guard", name)
		}
	}
}

type r91toolsRoundTripper func(*http.Request) (*http.Response, error)

func (f r91toolsRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TS 6141e6fefb mcp-http-transport.test.ts "should reject with a network
// error from protocol discovery without falling back to initialize".
func TestR91toolsDiscoveryNetworkErrorDoesNotFallBack(t *testing.T) {
	networkErr := errors.New("fetch failed")
	var mu sync.Mutex
	var methods []string
	httpClient := &http.Client{Transport: r91toolsRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			return &http.Response{StatusCode: http.StatusMethodNotAllowed, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
		}
		body, _ := io.ReadAll(r.Body)
		var msg struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &msg)
		mu.Lock()
		methods = append(methods, msg.Method)
		mu.Unlock()
		return nil, networkErr
	})}
	transport := NewHTTPTransport(HTTPTransportConfig{URL: "http://localhost:4000/mcp", HTTPClient: httpClient})
	client := NewMCPClient(transport, MCPClientConfig{})
	err := client.Connect(context.Background())
	if err == nil || !errors.Is(err, networkErr) {
		t.Fatalf("Connect err = %v, want the network error", err)
	}
	var mcpErr *MCPClientError
	if errors.As(err, &mcpErr) {
		t.Fatalf("err = %v is an MCPClientError", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 1 || methods[0] != "server/discover" {
		t.Fatalf("methods = %v, want only server/discover", methods)
	}
}

// TS 6309382f0e mcp-apps.test.ts: csp.baseUriDomains keeps its string
// elements (non-strings dropped), like the other domain lists.
func TestR91toolsMCPAppBaseURIDomains(t *testing.T) {
	res, err := GetMCPAppResourceFromReadResult("ui://app", ReadResourceResult{Contents: []ResourceContent{{
		URI: "ui://app", MimeType: MCPAppMimeType, Text: "<html></html>",
		Meta: map[string]interface{}{"ui": map[string]interface{}{"csp": map[string]interface{}{
			"baseUriDomains": []interface{}{"https://base.example", false},
		}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	csp, _ := res.Meta["csp"].(map[string]interface{})
	got, _ := csp["baseUriDomains"].([]string)
	if len(got) != 1 || got[0] != "https://base.example" {
		t.Fatalf("baseUriDomains = %#v", csp["baseUriDomains"])
	}
}
