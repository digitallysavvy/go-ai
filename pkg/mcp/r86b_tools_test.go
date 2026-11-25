package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type r86btoolsFixtureCase struct {
	Name string          `json:"name"`
	Raw  string          `json:"raw"`
	TS   json.RawMessage `json:"ts"`
}

func r86btoolsLoad(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatal(err)
	}
}

func r86btoolsRaw(t *testing.T, raw string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// V061 item 25 (+ the fetch Headers.get class): TS extractWWWAuthenticateParams
// reads the header through fetch's Headers.get (each byte one character) and
// matches /(?:^|[,\s])key="([^"]*)"/i with JS \s. Recorded with node fetch
// against the same raw bytes (testdata/r86b_tools_www_authenticate_ts.json).
func TestR86btoolsWWWAuthenticateMatchesTS(t *testing.T) {
	var fixture struct {
		Cases []r86btoolsFixtureCase `json:"cases"`
	}
	r86btoolsLoad(t, "testdata/r86b_tools_www_authenticate_ts.json", &fixture)
	if len(fixture.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range fixture.Cases {
		var want struct {
			ResourceMetadataURL *string `json:"resourceMetadataUrl"`
			Scope               *string `json:"scope"`
		}
		if err := json.Unmarshal(c.TS, &want); err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(r86btoolsRaw(t, c.Raw))), nil)
		if err != nil {
			t.Fatal(err)
		}
		got := ExtractWWWAuthenticateParams(resp)
		gotURL := ""
		if got.ResourceMetadataURL != nil {
			gotURL = got.ResourceMetadataURL.String()
		}
		wantURL, wantScope := "", ""
		if want.ResourceMetadataURL != nil {
			wantURL = *want.ResourceMetadataURL
		}
		if want.Scope != nil {
			wantScope = *want.Scope
		}
		if gotURL != wantURL || got.Scope != wantScope {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", c.Name, gotURL, got.Scope, wantURL, wantScope)
		}
	}
}

// TS verifyMCPEventSignature reads webhook-id / -timestamp / -signature
// through Headers.get: the signed prefix is the UTF-8 of the Latin-1 text,
// and a rotation signature on a second header line still verifies. Recorded
// with a node http server (testdata/r86b_tools_webhook_signature_ts.json).
func TestR86btoolsWebhookSignatureHeadersMatchTS(t *testing.T) {
	var fixture struct {
		Secret    string                 `json:"secret"`
		Timestamp int64                  `json:"timestamp"`
		Body      string                 `json:"body"`
		Cases     []r86btoolsFixtureCase `json:"cases"`
	}
	r86btoolsLoad(t, "testdata/r86b_tools_webhook_signature_ts.json", &fixture)
	if len(fixture.Cases) == 0 {
		t.Fatal("no cases")
	}
	old := mcpEventNow
	mcpEventNow = func() time.Time { return time.Unix(fixture.Timestamp, 0) }
	t.Cleanup(func() { mcpEventNow = old })
	for _, c := range fixture.Cases {
		var want struct {
			OK bool   `json:"ok"`
			ID string `json:"id"`
		}
		if err := json.Unmarshal(c.TS, &want); err != nil {
			t.Fatal(err)
		}
		req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(r86btoolsRaw(t, c.Raw))))
		if err != nil {
			t.Fatal(err)
		}
		ok, err := verifyMCPEventSignature(req.Header, []byte(fixture.Body), fixture.Secret)
		if err != nil || ok != want.OK {
			t.Errorf("%s: verified = %v (%v), want %v", c.Name, ok, err, want.OK)
		}
	}
}

// r86btoolsClient answers every POST with the given headers and a JSON-RPC
// result, recording the requests.
type r86btoolsClient struct {
	mu       sync.Mutex
	header   http.Header
	requests []*http.Request
}

func (c *r86btoolsClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req.Clone(req.Context()))
	c.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     c.header.Clone(),
		Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{}}`)),
	}, nil
}

// TS mcp-http-transport.ts response.headers.get('mcp-session-id') and
// get('content-type'): repeated lines are joined with ", " (fetch
// Headers.get, checked with node fetch: "a" and "b" read as "a, b"), so
// the session id echoed back is "a, b" and a content-type list holding
// application/json is parsed as JSON.
func TestR86btoolsHTTPTransportJoinsRepeatedHeaders(t *testing.T) {
	client := &r86btoolsClient{header: http.Header{
		"Content-Type":   {"text/plain", "application/json"},
		"Mcp-Session-Id": {"a", "b"},
	}}
	transport := NewHTTPTransport(HTTPTransportConfig{InitialProtocolVersion: LatestLegacyProtocolVersion,
		URL:       "http://localhost:9999/mcp",
		SSEClient: client,
	})
	transport.connected = true
	msg, err := CreateRequest(1, "initialize", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v (TS parses a content-type list holding application/json)", err)
	}
	if got := transport.SessionID(); got != "a, b" {
		t.Fatalf("SessionID() = %q, want %q", got, "a, b")
	}
}

// TS mcp-event-webhook.ts reads content-type, webhook-id and
// X-MCP-Subscription-Id through request.headers.get: each byte is one
// character (Node's http server hands header values over as Latin-1, as
// recorded in testdata/r86b_tools_webhook_signature_ts.json), so a raw 0xA0
// around the media type is trimmed by JS trim(), and a webhook-id byte 0xE9
// equals the event's "é" eventId.
func TestR86btoolsWebhookHeadersReadAsLatin1(t *testing.T) {
	h := newWebhookHarness(t)
	event := map[string]interface{}{}
	for k, v := range testEvent {
		event[k] = v
	}
	event["eventId"] = "evt_é"
	body, _ := json.Marshal(event)
	r := signedEventRequestBody(h.sub, body, "evt_é", fmt.Sprint(mcpEventNow().Unix()), h.sub.Delivery.Secret)
	r.Header.Set("webhook-id", "evt_\xe9")
	r.Header.Set("content-type", "\xa0application/json\xa0")
	if code := h.do(r); code != 204 || h.events != 1 {
		t.Fatalf("code=%d events=%d, want 204 and the event delivered", code, h.events)
	}
}

// TS fetchWithValidatedRedirects follows OAuth discovery redirects with
// redirect: 'manual' and new URL(response.headers.get('location'), current):
// raw UTF-8 in Location reads as Latin-1 (node: /caf%C3%83%C2%A9) and repeated
// Location lines are joined with ", " (/a,%20/b).
func TestR86btoolsOAuthRedirectLocationReadsAsFetch(t *testing.T) {
	for _, c := range []struct {
		location []string
		want     string
	}{
		{[]string{"/caf\xc3\xa9"}, "/caf%C3%83%C2%A9"},
		{[]string{"/a", "/b"}, "/a,%20/b"},
	} {
		var mu sync.Mutex
		var paths []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			paths = append(paths, r.URL.EscapedPath())
			n := len(paths)
			mu.Unlock()
			if n == 1 {
				w.Header()["Location"] = c.location
				w.WriteHeader(http.StatusFound)
				return
			}
			_, _ = w.Write([]byte(`{}`))
		}))
		resp, err := oauthMetadataGET(context.Background(), srv.Client(), srv.URL+"/start", "", srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		srv.Close()
		mu.Lock()
		if len(paths) != 2 || paths[1] != c.want {
			t.Errorf("Location %q: requested %q, want the second hop at %q", c.location, paths, c.want)
		}
		mu.Unlock()
	}
}

type r86btoolsBodyTransport struct{ bodies []string }

func (rt *r86btoolsBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	b, _ := io.ReadAll(req.Body)
	rt.bodies = append(rt.bodies, string(b))
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"AT","token_type":"Bearer"}`)), Request: req}, nil
}

// cr-r86b-prova-to-tools 3: TS token requests send a URLSearchParams body:
// insertion order (grant_type, code, code_verifier, redirect_uri, then
// client auth, then resource) and form encoding with `*` literal and `~` as
// %7E. Go sent url.Values.Encode() (sorted, `*` as %2A, `~` literal).
// Expected bodies from node URLSearchParams.toString() with the same steps.
func TestR86btoolsOAuthTokenBodyMatchesURLSearchParams(t *testing.T) {
	rt := &r86btoolsBodyTransport{}
	client := &http.Client{Transport: rt}
	meta := &OAuthAuthorizationServerMetadata{TokenEndpoint: "https://auth.example.com/token"}
	resource, _ := url.Parse("https://api.example.com/mcp")
	if _, err := ExchangeOAuthAuthorization(context.Background(), "https://auth.example.com", ExchangeOAuthAuthorizationParams{
		Metadata: meta, ClientInformation: OAuthClientInformation{ClientID: "cid", ClientSecret: "s"},
		AuthorizationCode: "c*~ d", CodeVerifier: "v~.-_", RedirectURI: "https://app.example.com/cb", Resource: resource, HTTPClient: client,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := RefreshOAuthAuthorization(context.Background(), "https://auth.example.com", RefreshOAuthAuthorizationParams{
		Metadata: meta, RefreshToken: "r~*", HTTPClient: client,
		AddClientAuthentication: func(_ context.Context, _ http.Header, p url.Values, _ string, _ *OAuthAuthorizationServerMetadata) error {
			// TS keeps a callback's insertion order; Go's url.Values cannot,
			// so client_id / client_secret come first and other keys sorted.
			p.Set("client_id", "cid")
			p.Set("x_extra", "1")
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"grant_type=authorization_code&code=c*%7E+d&code_verifier=v%7E.-_&redirect_uri=https%3A%2F%2Fapp.example.com%2Fcb&client_id=cid&client_secret=s&resource=https%3A%2F%2Fapi.example.com%2Fmcp",
		"grant_type=refresh_token&refresh_token=r%7E*&client_id=cid&x_extra=1",
	}
	if len(rt.bodies) != 2 || rt.bodies[0] != want[0] || rt.bodies[1] != want[1] {
		t.Fatalf("bodies = %q, want %q", rt.bodies, want)
	}
}

// TS mcp-events.ts url.searchParams.set(MCP_EVENT_CALLBACK_KEY, key)
// re-serializes the query (node: "?tenant=one&x=a+b&y=%7E*%7E&...") and
// replaces an existing key in place.
func TestR86btoolsEventCallbackQueryMatchesURLSearchParams(t *testing.T) {
	for raw, want := range map[string]string{
		"tenant=one&x=a%20b&y=~*%7e":                               "tenant=one&x=a+b&y=%7E*%7E&mcp_event_subscription=KEY",
		"mcp_event_subscription=old&x=1&mcp_event_subscription=o2": "mcp_event_subscription=KEY&x=1",
		"": "mcp_event_subscription=KEY",
	} {
		if got := appendMCPEventQuery(raw, MCPEventCallbackKey, "KEY"); got != want {
			t.Errorf("%q: got %q, want %q", raw, got, want)
		}
	}
}
