package mcp

import (
	"context"
	"net/http"
	"testing"
)

// TestR97integResponseJSONKeepsPrototypeKey: TS reads these bodies with
// `await response.json()` (plain JSON.parse), so a __proto__ key or a
// constructor.prototype is an ordinary property: the OAuth protected
// resource metadata (oauth.ts:605), the authorization server metadata
// (754 / 759), the token response (1106 / 1211), the client registration
// (1274), an application/json POST response's error.data
// (mcp-http-transport.ts:390) and a result re-read after the transport. The
// secure paths (SSE, stdio, webhook) keep rejecting.
func TestR97integResponseJSONKeepsPrototypeKey(t *testing.T) {
	const proto = `"__proto__":{"a":1},"constructor":{"prototype":{}},`
	t.Run("protected resource metadata", func(t *testing.T) {
		client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{`+proto+`"resource":"https://resource.example.com"}`), nil
		})
		md, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://resource.example.com", OAuthDiscoveryOptions{HTTPClient: client})
		if err != nil || md.Resource != "https://resource.example.com" {
			t.Fatalf("md = %+v, err = %v", md, err)
		}
	})
	t.Run("authorization server metadata", func(t *testing.T) {
		client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{`+proto+`"issuer":"https://auth.example.com","authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"https://auth.example.com/token","response_types_supported":["code"]}`), nil
		})
		md, err := DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com", OAuthDiscoveryOptions{HTTPClient: client})
		if err != nil || md == nil || md.Issuer != "https://auth.example.com" {
			t.Fatalf("md = %+v, err = %v", md, err)
		}
	})
	t.Run("token response", func(t *testing.T) {
		tokens, err := decodeOAuthTokenResponse(jsonResponse(http.StatusOK, `{`+proto+`"access_token":"a","token_type":"Bearer"}`))
		if err != nil || tokens.AccessToken != "a" {
			t.Fatalf("tokens = %+v, err = %v", tokens, err)
		}
	})
	t.Run("client registration", func(t *testing.T) {
		client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{`+proto+`"client_id":"c","redirect_uris":["https://app/cb"]}`), nil
		})
		info, err := RegisterOAuthClient(context.Background(), "https://auth.example.com", nil, OAuthClientMetadata{RedirectURIs: []string{"https://app/cb"}}, client)
		if err != nil || info == nil || info.ClientID != "c" {
			t.Fatalf("info = %+v, err = %v", info, err)
		}
	})
	t.Run("http json error data", func(t *testing.T) {
		msgs, err := parseMCPHTTPJSONMessages([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"m","data":{` + proto + `"k":1}}}`))
		if err != nil || len(msgs) != 1 || msgs[0].Error == nil {
			t.Fatalf("msgs = %#v, err = %v", msgs, err)
		}
		data, _ := msgs[0].Error.Data.(map[string]interface{})
		if data["k"] != float64(1) {
			t.Fatalf("error.data = %#v", msgs[0].Error.Data)
		}
	})
	t.Run("typed results with nested UnmarshalJSON", func(t *testing.T) {
		var res CallToolResult
		if err := unmarshalPlainJSON([]byte(`{`+proto+`"content":[{"type":"text","text":"x","__proto__":{}}]}`), &res); err != nil || len(res.Content) != 1 {
			t.Fatalf("CallToolResult = %+v, err = %v", res, err)
		}
		var init InitializeResult
		if err := unmarshalPlainJSON([]byte(`{"protocolVersion":"2025-06-18","capabilities":{`+proto+`"tools":{}},"serverInfo":{"name":"s","version":"1","__proto__":{}}}`), &init); err != nil || init.ServerInfo.Name != "s" {
			t.Fatalf("InitializeResult = %+v, err = %v", init, err)
		}
		if err := unmarshalSafeJSON([]byte(`{"content":[{"type":"text","text":"x","__proto__":{}}]}`), &res); err == nil {
			t.Fatal("secure decode accepted a nested __proto__ key")
		}
	})
	t.Run("result re-read", func(t *testing.T) {
		var v map[string]interface{}
		if err := unmarshalPlainJSON([]byte(`{`+proto+`"k":1}`), &v); err != nil || v["k"] != float64(1) {
			t.Fatalf("v = %v, err = %v", v, err)
		}
		if err := unmarshalSafeJSON([]byte(`{`+proto+`"k":1}`), &v); err == nil {
			t.Fatal("secure decode accepted a __proto__ key")
		}
	})
}
