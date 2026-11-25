package mcp

import (
	"context"
	"sync"
	"testing"
	"time"
)

// The OAuthConfig refresh runs under the first caller's ctx. When that caller
// gives up, a waiter whose own ctx is live must not get the leader's
// cancellation: it runs the refresh again (as transportAuth.authorizeOnce).
func TestOAuthRefreshWaiterRerunsWhenLeaderCanceled(t *testing.T) {
	newCfg := func(started chan struct{}) *OAuthConfig {
		var mu sync.Mutex
		calls := 0
		return &OAuthConfig{AccessToken: "old", RefreshTokenFunc: func(ctx context.Context, _ *OAuthConfig) (string, time.Duration, error) {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				close(started)
				<-ctx.Done()
				return "", 0, ctx.Err()
			}
			return "new", time.Hour, nil
		}}
	}
	for _, name := range []string{"http", "sse"} {
		t.Run(name, func(t *testing.T) {
			started := make(chan struct{})
			var refresh func(context.Context) error
			if name == "http" {
				refresh = NewHTTPTransport(HTTPTransportConfig{URL: "https://example.com/mcp", OAuth: newCfg(started)}).refreshOAuthToken
			} else {
				refresh = NewSSETransport(SSETransportConfig{URL: "https://example.com/sse", OAuth: newCfg(started)}).refreshOAuthToken
			}
			leaderCtx, cancelLeader := context.WithCancel(context.Background())
			leaderDone := make(chan error, 1)
			go func() { leaderDone <- refresh(leaderCtx) }()
			<-started
			waiterDone := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				waiterDone <- refresh(ctx)
			}()
			time.Sleep(20 * time.Millisecond) // let the waiter join the flight
			cancelLeader()
			if err := <-leaderDone; err == nil {
				t.Fatal("leader refresh succeeded, want its cancellation")
			}
			if err := <-waiterDone; err != nil {
				t.Fatalf("waiter err = %v, want a rerun refresh to succeed", err)
			}
		})
	}
}
