package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TS scheduleInboundSseReconnection reads this.lastInboundEventId when the
// backoff timer fires, so an event another inbound GET received during the
// backoff is the one the reconnect resumes from.
func TestHTTPTransportReconnectResumesFromEventIDAtFireTime(t *testing.T) {
	resumed := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if id := r.Header.Get("Last-Event-Id"); id != "" {
				resumed <- id
			}
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	transport := NewHTTPTransport(HTTPTransportConfig{URL: server.URL, InitialProtocolVersion: LatestLegacyProtocolVersion})
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer transport.Close() //nolint:errcheck

	transport.sseMu.Lock()
	transport.lastInboundEventID = "before-backoff"
	transport.sseMu.Unlock()
	transport.scheduleInboundSSEReconnection()
	transport.sseMu.Lock()
	transport.lastInboundEventID = "during-backoff"
	transport.sseMu.Unlock()

	select {
	case id := <-resumed:
		if id != "during-backoff" {
			t.Fatalf("reconnect Last-Event-Id = %q, want during-backoff", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no reconnect GET")
	}
}

// firstBlocksRoundTripper blocks the first request until its context ends and
// passes later ones to next.
type firstBlocksRoundTripper struct {
	calls   atomic.Int64
	started chan struct{}
	next    http.RoundTripper
}

func (f *firstBlocksRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if f.calls.Add(1) == 1 {
		close(f.started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	return f.next.RoundTrip(r)
}

// TS authorizeOnce shares one auth() promise that no request's abort signal
// cancels, so a request that joined the flow is not failed by another
// request giving up. Go runs the shared flow under the first caller's ctx: a
// caller still waiting when that ctx is canceled runs the flow again.
func TestTransportAuthJoinedCallerSurvivesLeaderCancel(t *testing.T) {
	provider, oauthClient := refreshingAuthFixture(t)
	rt := &firstBlocksRoundTripper{started: make(chan struct{}), next: oauthClient.Transport}
	auth := newTransportAuth(provider, "https://auth.example.com/mcp", &http.Client{Transport: rt})

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() {
		_, err := auth.authorizeOnce(leaderCtx, nil, "")
		leaderErr <- err
	}()
	select {
	case <-rt.started:
	case <-time.After(3 * time.Second):
		t.Fatal("auth flow never started")
	}

	type outcome struct {
		result AuthResult
		err    error
	}
	follower := make(chan outcome, 1)
	go func() {
		r, err := auth.authorizeOnce(context.Background(), nil, "")
		follower <- outcome{r, err}
	}()
	time.Sleep(100 * time.Millisecond) // let the follower join the flight
	cancelLeader()
	if err := <-leaderErr; err == nil {
		t.Fatal("leader: want its own cancellation")
	}
	select {
	case got := <-follower:
		if got.err != nil || got.result != AuthResultAuthorized {
			t.Fatalf("follower = %v, %v; want AUTHORIZED", got.result, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("follower hung")
	}
}

// NewSSETransport applies its timeout and redirect policy to a copy of the
// caller's client, as NewHTTPTransport does: the caller's client (perhaps
// http.DefaultClient) is left unchanged.
func TestSSETransportDoesNotMutateCallerHTTPClient(t *testing.T) {
	caller := &http.Client{}
	tr := NewSSETransport(SSETransportConfig{URL: "http://localhost:9999/sse", TimeoutMS: 1500, HTTPClient: caller})
	if caller.Timeout != 0 || caller.CheckRedirect != nil {
		t.Fatalf("caller client changed: timeout %v, CheckRedirect set %v", caller.Timeout, caller.CheckRedirect != nil)
	}
	if tr.client == caller || tr.client.Timeout != 1500*time.Millisecond || tr.client.CheckRedirect == nil {
		t.Fatalf("transport client = %+v", tr.client)
	}
}
