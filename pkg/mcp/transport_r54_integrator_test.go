package mcp

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// A request already waiting on a shared OAuth flow that then panics gets an
// error, not an empty result with a nil error.
func TestR54IntegratorWaiterOnPanickedAuthFlowGetsError(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	client := &http.Client{Transport: oauthRoundTripFunc(func(*http.Request) (*http.Response, error) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		panic("auth transport exploded")
	})}
	a := newTransportAuth(newFakeOAuthProvider(), "https://auth.example.com/mcp", client)
	runnerDone := make(chan struct{})
	go func() {
		defer close(runnerDone)
		defer func() { _ = recover() }()
		_, _ = a.authorizeOnce(context.Background(), nil, "")
	}()
	<-entered
	waiterErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := a.authorizeOnce(ctx, nil, "")
		waiterErr <- err
	}()
	time.Sleep(50 * time.Millisecond) // let the waiter block on the flight
	close(release)
	<-runnerDone
	if err := <-waiterErr; !errors.Is(err, errAuthFlowPanicked) {
		t.Fatalf("waiter err = %v, want errAuthFlowPanicked", err)
	}
}

// Same for the OAuthConfig single-flight refresh on both transports.
func TestR54IntegratorWaiterOnPanickedRefreshGetsError(t *testing.T) {
	for _, name := range []string{"http", "sse"} {
		t.Run(name, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			cfg := &OAuthConfig{AccessToken: "old", RefreshTokenFunc: func(context.Context, *OAuthConfig) (string, time.Duration, error) {
				close(entered)
				<-release
				panic("refresh exploded")
			}}
			var refresh func(context.Context) error
			if name == "http" {
				refresh = NewHTTPTransport(HTTPTransportConfig{URL: "https://example.com/mcp", OAuth: cfg}).refreshOAuthToken
			} else {
				refresh = NewSSETransport(SSETransportConfig{URL: "https://example.com/sse", OAuth: cfg}).refreshOAuthToken
			}
			runnerDone := make(chan struct{})
			go func() {
				defer close(runnerDone)
				defer func() { _ = recover() }()
				_ = refresh(context.Background())
			}()
			<-entered
			waiterErr := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				waiterErr <- refresh(ctx)
			}()
			time.Sleep(50 * time.Millisecond) // let the waiter block on the refresh
			close(release)
			<-runnerDone
			if err := <-waiterErr; !errors.Is(err, errOAuthRefreshPanicked) {
				t.Fatalf("waiter err = %v, want errOAuthRefreshPanicked", err)
			}
		})
	}
}
