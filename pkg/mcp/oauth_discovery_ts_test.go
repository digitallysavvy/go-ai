package mcp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// Ports oauth.test.ts "discoverOAuthProtectedResourceMetadata" and
// "discoverAuthorizationServerMetadata" cases that were not yet covered
// (status-code fallback table, CORS/TypeError retry without the protocol
// header, 5xx handling, root-path no-fallback).

const tsPRM = `{"resource":"https://resource.example.com","authorization_servers":["https://auth.example.com"]}`

type recordedCall struct {
	url    string
	header string
}

func recordingClient(t *testing.T, fn func(n int, req *http.Request) (*http.Response, error)) (*http.Client, *[]recordedCall) {
	t.Helper()
	var calls []recordedCall
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, recordedCall{req.URL.String(), req.Header.Get("MCP-Protocol-Version")})
		return fn(len(calls), req)
	})
	return client, &calls
}

func TestTSPRMDiscoveryFallsBackOnEach4xx(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 410, 422, 429} {
		client, calls := recordingClient(t, func(n int, _ *http.Request) (*http.Response, error) {
			if n == 1 {
				return jsonResponse(status, ``), nil
			}
			return jsonResponse(200, tsPRM), nil
		})
		md, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://resource.example.com/path/name", OAuthDiscoveryOptions{HTTPClient: client})
		if err != nil || md.Resource != "https://resource.example.com" {
			t.Fatalf("%d: md=%+v err=%v", status, md, err)
		}
		want := []recordedCall{
			{"https://resource.example.com/.well-known/oauth-protected-resource/path/name", ProtocolVersion},
			{"https://resource.example.com/.well-known/oauth-protected-resource", ProtocolVersion},
		}
		if len(*calls) != 2 || (*calls)[0] != want[0] || (*calls)[1] != want[1] {
			t.Fatalf("%d: calls = %+v", status, *calls)
		}
	}
}

func TestTSPRMDiscoveryErrorsAndNoFallback(t *testing.T) {
	cases := []struct {
		name      string
		server    string
		statuses  []int
		wantErr   string
		wantCalls int
	}{
		{"404 at root", "https://resource.example.com", []int{404}, "Resource server does not implement OAuth 2.0 Protected Resource Metadata.", 1},
		{"500 at root", "https://resource.example.com", []int{500}, "HTTP 500", 1},
		{"500 with path does not fall back", "https://resource.example.com/path/name", []int{500}, "HTTP 500", 1},
		{"404 for both path-aware and root", "https://resource.example.com/path/name", []int{404, 404}, "Resource server does not implement OAuth 2.0 Protected Resource Metadata.", 2},
		{"root path", "https://resource.example.com/", []int{404}, "Resource server does not implement", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client, calls := recordingClient(t, func(n int, _ *http.Request) (*http.Response, error) {
				return jsonResponse(c.statuses[n-1], ``), nil
			})
			_, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), c.server, OAuthDiscoveryOptions{HTTPClient: client})
			if err == nil || !strings.Contains(err.Error(), c.wantErr) || len(*calls) != c.wantCalls {
				t.Fatalf("err = %v, calls = %d", err, len(*calls))
			}
		})
	}
	client, _ := recordingClient(t, func(int, *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"scopes_supported":["email","mcp"]}`), nil
	})
	if _, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://resource.example.com", OAuthDiscoveryOptions{HTTPClient: client}); err == nil {
		t.Fatal("metadata missing required fields must be rejected")
	}
}

func TestTSPRMDiscoveryPreservesQuery(t *testing.T) {
	client, calls := recordingClient(t, func(int, *http.Request) (*http.Response, error) { return jsonResponse(200, tsPRM), nil })
	if _, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://resource.example.com/path?param=value", OAuthDiscoveryOptions{HTTPClient: client}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].url != "https://resource.example.com/.well-known/oauth-protected-resource/path?param=value" {
		t.Fatalf("calls = %+v", *calls)
	}
}

func TestTSPRMDiscoveryRetriesNetworkFailureWithoutProtocolHeader(t *testing.T) {
	// "returns metadata when first fetch fails but second without MCP header succeeds"
	client, calls := recordingClient(t, func(n int, _ *http.Request) (*http.Response, error) {
		if n == 1 {
			return nil, errors.New("Network error")
		}
		return jsonResponse(200, tsPRM), nil
	})
	md, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://resource.example.com", OAuthDiscoveryOptions{HTTPClient: client})
	if err != nil || md.Resource == "" {
		t.Fatalf("md=%+v err=%v", md, err)
	}
	if len(*calls) != 2 || (*calls)[0].header == "" || (*calls)[1].header != "" {
		t.Fatalf("calls = %+v, want header on the first call only", *calls)
	}

	// "falls back when path-aware discovery encounters CORS error": failure,
	// retry without header (404), then the root fallback succeeds.
	client, calls = recordingClient(t, func(n int, _ *http.Request) (*http.Response, error) {
		switch n {
		case 1:
			return nil, errors.New("CORS error")
		case 2:
			return jsonResponse(404, ``), nil
		}
		return jsonResponse(200, tsPRM), nil
	})
	if _, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://resource.example.com/deep/path", OAuthDiscoveryOptions{HTTPClient: client}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 3 || (*calls)[2].url != "https://resource.example.com/.well-known/oauth-protected-resource" || (*calls)[2].header != ProtocolVersion {
		t.Fatalf("calls = %+v", *calls)
	}

	// Both attempts failing counts as "no metadata" (TS: undefined response).
	client, calls = recordingClient(t, func(int, *http.Request) (*http.Response, error) { return nil, errors.New("down") })
	_, err = DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://resource.example.com", OAuthDiscoveryOptions{HTTPClient: client})
	if err == nil || !strings.Contains(err.Error(), "does not implement OAuth 2.0 Protected Resource Metadata") || len(*calls) != 2 {
		t.Fatalf("err = %v, calls = %d", err, len(*calls))
	}
}

func TestTSASDiscoveryNetworkFailuresAndStatuses(t *testing.T) {
	const asMeta = `{"issuer":"https://auth.example.com","authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"https://auth.example.com/token","registration_endpoint":"https://auth.example.com/register","response_types_supported":["code"],"code_challenge_methods_supported":["S256"]}`
	// "handles CORS errors with retry"
	client, calls := recordingClient(t, func(n int, _ *http.Request) (*http.Response, error) {
		if n == 1 {
			return nil, errors.New("CORS error")
		}
		return jsonResponse(200, asMeta), nil
	})
	md, err := DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com", OAuthDiscoveryOptions{HTTPClient: client})
	if err != nil || md == nil || md.Issuer != "https://auth.example.com" {
		t.Fatalf("md=%+v err=%v", md, err)
	}
	if len(*calls) != 2 || (*calls)[0].header == "" || (*calls)[1].header != "" {
		t.Fatalf("calls = %+v", *calls)
	}

	// "returns undefined when all URLs fail with CORS errors": 4 URLs x 2 attempts.
	client, calls = recordingClient(t, func(int, *http.Request) (*http.Response, error) { return nil, errors.New("CORS error") })
	md, err = DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com/tenant1", OAuthDiscoveryOptions{HTTPClient: client})
	if err != nil || md != nil || len(*calls) != 8 {
		t.Fatalf("md=%+v err=%v calls=%d", md, err, len(*calls))
	}

	// "throws on non-4xx errors"
	client, _ = recordingClient(t, func(int, *http.Request) (*http.Response, error) { return jsonResponse(500, ``), nil })
	if _, err = DiscoverAuthorizationServerMetadata(context.Background(), "https://mcp.example.com", OAuthDiscoveryOptions{HTTPClient: client}); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v", err)
	}

	// custom protocol version
	client, calls = recordingClient(t, func(int, *http.Request) (*http.Response, error) { return jsonResponse(200, asMeta), nil })
	if _, err = DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com", OAuthDiscoveryOptions{HTTPClient: client, ProtocolVersion: "2025-01-01"}); err != nil || (*calls)[0].header != "2025-01-01" {
		t.Fatalf("err=%v calls=%+v", err, *calls)
	}
}
