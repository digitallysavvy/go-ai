package mcp

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

// TS authorizeOnce is not tied to the abort signal: a Close while the 401
// recovery runs does not silence it, and its UnauthorizedError still reaches
// onerror and rejects start().
func TestSSETransportAuthFailureAfterCloseIsReported(t *testing.T) {
	provider := newFakeOAuthProvider()
	inAuth := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	oauthClient := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(*http.Request) (*http.Response, error) {
			once.Do(func() { close(inAuth) })
			<-release
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/register": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusCreated, `{"client_id":"c","redirect_uris":["https://app.example.com/callback"]}`), nil
		},
	})
	var mu sync.Mutex
	var reported []error
	transport := NewSSETransport(SSETransportConfig{
		URL: "https://auth.example.com/mcp", SSEClient: &authServerSSEClient{want: "Bearer never"},
		AuthProvider: provider, AuthHTTPClient: oauthClient,
		OnError: func(err error) {
			mu.Lock()
			reported = append(reported, err)
			mu.Unlock()
		},
	})
	done := make(chan error, 1)
	go func() { done <- transport.Connect(context.Background()) }()
	<-inAuth
	_ = transport.Close()
	close(release)
	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Connect hung")
	}
	var unauthorized *UnauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Fatalf("Connect err = %v, want UnauthorizedError", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 1 || !errors.As(reported[0], &unauthorized) {
		t.Fatalf("OnError got %v, want the UnauthorizedError once", reported)
	}
	if transport.IsConnected() {
		t.Fatal("connected after Close")
	}
}

// A Connect that starts while Close is still tearing down (here held by a
// POST-response reader Close waits for) must not be undone by that Close:
// it waits for the teardown, then starts, and stays connected with a live
// inbound SSE lifecycle. Before startMu, Close cleared connected after the
// Connect had returned nil and started its inbound GET (v0.5.2 too).
func TestHTTPTransportConnectDuringCloseTeardownStaysConnected(t *testing.T) {
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:                    "http://example.test/mcp",
		InitialProtocolVersion: "2025-06-18",
		SSEClient: sseClientFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	})
	transport.postSSEWG.Add(1) // a POST-response reader Close must wait for
	closed := make(chan struct{})
	go func() { _ = transport.Close(); close(closed) }()
	if !waitFor(2*time.Second, func() bool {
		transport.mu.Lock()
		defer transport.mu.Unlock()
		return transport.closeGen == 1
	}) {
		t.Fatal("Close did not start")
	}
	connected := make(chan error, 1)
	go func() { connected <- transport.Connect(context.Background()) }()
	time.Sleep(50 * time.Millisecond)
	transport.postSSEWG.Done()
	<-closed
	var err error
	select {
	case err = <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("Connect hung")
	}
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	transport.sseMu.Lock()
	live := transport.sseLifecycleCtx != nil && !transport.sseClosing && transport.sseLifecycleCtx.Err() == nil
	transport.sseMu.Unlock()
	if !transport.IsConnected() || !live {
		t.Fatalf("Connect returned nil but connected=%v, inbound lifecycle live=%v", transport.IsConnected(), live)
	}
	_ = transport.Close()
}
