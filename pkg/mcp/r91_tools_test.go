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
	"time"
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

// core3-08 (undici fetch httpRedirectFetch deletes authorization,
// proxy-authorization and cookie on a cross-origin hop): a redirect from
// 127.0.0.1:A to 127.0.0.1:B (same host, other port: another origin) must
// carry none of them.
func TestR91toolsMCPRedirectDropsCredentialHeaders(t *testing.T) {
	var got http.Header
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	t.Cleanup(target.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/next", http.StatusFound)
	}))
	t.Cleanup(origin.Close)
	client := &http.Client{CheckRedirect: mcpFetchRedirectPolicy}
	req, _ := http.NewRequest(http.MethodGet, origin.URL, nil)
	req.Header.Set("Authorization", "Bearer x")
	req.Header.Set("Proxy-Authorization", "Basic x")
	req.Header.Set("Cookie", "s=1")
	req.Header.Set("X-Other", "kept")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	for _, h := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
		if got.Get(h) != "" {
			t.Errorf("%s reached the other origin", h)
		}
	}
	if got.Get("X-Other") != "kept" {
		t.Error("other headers must be kept")
	}
}

// tools-01 (TS mcp-client.ts:1044 requestOnce): the transport's
// MCPClientError reaches the caller unwrapped, and a request aborted
// mid-flight is MCPClientError "Request was aborted" with the abort reason.
func TestR91toolsMCPSendErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&m)
		if m["id"] == nil {
			w.WriteHeader(202)
			return
		}
		args, _ := m["params"].(map[string]interface{})
		switch {
		case m["method"] == "initialize":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(mustJSON(m["id"])) + `,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"s","version":"1"}}}`))
		case args["name"] == "boom":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
		default:
			select {
			case <-time.After(500 * time.Millisecond):
			case <-r.Context().Done():
			}
		}
	}))
	defer srv.Close()
	f := false
	c := NewMCPClient(NewHTTPTransport(HTTPTransportConfig{URL: srv.URL}), MCPClientConfig{ProtocolVersionDiscovery: &f})
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Close() //nolint:errcheck

	_, err := c.CallTool(context.Background(), "boom", nil)
	var mcpErr *MCPClientError
	if !errors.As(err, &mcpErr) || err.Error() != "MCP HTTP Transport Error: POSTing to endpoint (HTTP 500): boom" {
		t.Fatalf("HTTP 500 err = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err = c.CallTool(ctx, "slow", nil)
	mcpErr = nil
	if !errors.As(err, &mcpErr) || mcpErr.Message != "Request was aborted" || !errors.Is(err, context.Canceled) {
		t.Fatalf("abort err = %v (%T)", err, err)
	}
}

// tools-04 (TS mcp-client.ts:1075): the wire of a legacy cursor-less
// tools/list and of a modern tools/call follows the TS key order: no params
// key for the first page, {method, params, jsonrpc, id}, and the original
// params keys before _meta, whose three keys follow the caller's.
func TestR91toolsMCPWireKeyOrder(t *testing.T) {
	c := &MCPClient{protocolEra: "legacy"}
	params, _ := c.prepareRequestParams("tools/list", nil)
	msg, err := CreateRequest(1, "tools/list", params)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := json.Marshal(msg); string(data) != `{"method":"tools/list","jsonrpc":"2.0","id":1}` {
		t.Errorf("legacy tools/list = %s", data)
	}
	c = &MCPClient{protocolEra: "modern", protocolVersion: "v"}
	params, _ = c.prepareRequestParams("tools/call", map[string]interface{}{"name": "t", "arguments": map[string]interface{}{}, "_meta": map[string]interface{}{"x": 1}})
	msg, _ = CreateRequest(2, "tools/call", params)
	data, _ := json.Marshal(msg)
	want := `{"method":"tools/call","params":{"_meta":{"x":1,"io.modelcontextprotocol/protocolVersion":"v","io.modelcontextprotocol/clientCapabilities":`
	if !strings.HasPrefix(string(data), want) || !strings.HasSuffix(string(data), `},"jsonrpc":"2.0","id":2}`) {
		t.Errorf("modern tools/call = %s", data)
	}
	params, _ = c.prepareRequestParams("tools/call", json.RawMessage(`{"name":"t","arguments":{}}`))
	msg, _ = CreateRequest(3, "tools/call", params)
	data, _ = json.Marshal(msg)
	if !strings.HasPrefix(string(data), `{"method":"tools/call","params":{"name":"t","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"v",`) {
		t.Errorf("modern tools/call order = %s", data)
	}
}

// tools-05 (TS retry-with-exponential-backoff.ts:43): no backoff cap.
func TestR91toolsMCPToolCallRetryUncapped(t *testing.T) {
	cfg := mcpToolCallRetryConfig(7)
	if cfg.MaxDelay != 0 || cfg.InitialDelay != 2*time.Second || cfg.Multiplier != 2 {
		t.Fatalf("cfg = %+v", cfg)
	}
}

// tools-06 (TS mcp-client.ts:1033 requestOnce): a closed client reports
// "Attempted to send a request from a closed client" before any capability
// error, for completions and events too; an open client still reports the
// missing capability.
func TestR91toolsMCPClosedBeforeCapability(t *testing.T) {
	tr := newLifecycleTransport(ServerCapabilities{})
	client := NewMCPClient(tr, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(context.Background(), CompleteRequestParams{}); err == nil || err.Error() != "Server does not support completions" {
		t.Fatalf("open Complete err = %v", err)
	}
	_ = client.Close()
	if _, err := client.Complete(context.Background(), CompleteRequestParams{}); err == nil || !strings.Contains(err.Error(), "Attempted to send a request from a closed client") {
		t.Fatalf("closed Complete err = %v", err)
	}
	if err := client.eventsCall(context.Background(), "events/list", nil, nil); err == nil || !strings.Contains(err.Error(), "Attempted to send a request from a closed client") {
		t.Fatalf("closed events err = %v", err)
	}
}
