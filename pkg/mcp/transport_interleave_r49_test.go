package mcp

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// SSETransport states (r49 integrator model). The transport state is
// (streamGen, cancel/streamCtx, streamBody, connected/endpoint, streamErr).
//
//	Idle        gen=g, no cancel, no body, !connected
//	Handshake   Connect bumped gen to g+1 and installed cancel; GET pending
//	Reading     body installed, reader goroutine for g+1 running, no endpoint
//	Connected   endpoint locked, connected; reader running
//	Ended       reader saw EOF or a read error: !connected, streamErr set;
//	            cancel and body stay until the next Close or Connect
//
// Transitions, each guarded by the gen captured at Connect's start:
//
//	Connect      Idle/Ended → Handshake (bumps gen, aborts the previous
//	             request or stream); Connected → no-op
//	GET done     Handshake → Reading, only if gen is unchanged; otherwise
//	             the late body is closed and Connect fails
//	GET failed   Handshake → Idle (releaseHandshake(gen)); stale → no-op
//	endpoint     Reading → Connected, only if gen is unchanged
//	server EOF   Reading/Connected → Ended (gen unchanged) or no-op (stale)
//	Close        any → Idle (bumps gen, cancels, closes body)
//	ctx cancel   Handshake → GET fails → Idle; Reading → closeStream(gen)
//	             → Idle (only if gen is unchanged); Connected → no effect
//	2nd Connect  Handshake/Reading of g → Handshake of g+1; the first
//	             Connect returns the closed-during-connect error
//
// Invariants: Connected implies endpoint, body, cancel and a live
// streamCtx; after Close nothing is installed; every reader and every
// Connect returns once Close and the callers' contexts end; the transport
// can always connect again.

// interleaveSSEServer serves the legacy SSE GET with a randomly chosen
// behavior per request and counts the streams it holds open.
type interleaveSSEServer struct {
	srv    *httptest.Server
	active atomic.Int64
	mu     sync.Mutex
	rng    *rand.Rand
	eof    chan struct{}
	forced int // -1: random mode
}

func newInterleaveSSEServer(seed int64) *interleaveSSEServer {
	s := &interleaveSSEServer{rng: rand.New(rand.NewSource(seed)), eof: make(chan struct{}), forced: -1}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

func (s *interleaveSSEServer) pick() (mode int, delay time.Duration, eof chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mode = s.forced
	if mode < 0 {
		mode = s.rng.Intn(5)
	}
	return mode, time.Duration(s.rng.Intn(4)) * time.Millisecond, s.eof
}

// endStreams makes every stream open now end (server EOF).
func (s *interleaveSSEServer) endStreams() {
	s.mu.Lock()
	close(s.eof)
	s.eof = make(chan struct{})
	s.mu.Unlock()
}

func (s *interleaveSSEServer) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	s.active.Add(1)
	defer s.active.Add(-1)
	mode, delay, eof := s.pick()
	// The handshake stays pending for a while.
	select {
	case <-time.After(delay):
	case <-r.Context().Done():
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher := w.(http.Flusher)
	flusher.Flush()
	switch mode {
	case 0: // endpoint at once
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /messages\n\n")
	case 1: // endpoint after a delay
		select {
		case <-time.After(delay + time.Millisecond):
		case <-r.Context().Done():
			return
		case <-eof:
			return
		}
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /messages\n\n")
	case 2: // quiet: never sends the endpoint
	case 3: // EOF before the endpoint
		return
	case 4: // endpoint, then a message
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /messages\n\n")
		_, _ = fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/ping\"}\n\n")
	}
	flusher.Flush()
	select {
	case <-r.Context().Done():
	case <-eof:
	case <-time.After(30 * time.Second): // a leaked stream fails the active check first
	}
}

// countGoroutines counts goroutines whose stack contains any of fns.
func countGoroutines(fns ...string) int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	count := 0
	for _, g := range strings.Split(string(buf), "\n\n") {
		for _, fn := range fns {
			if strings.Contains(g, fn) {
				count++
				break
			}
		}
	}
	return count
}

func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
	return true
}

func sseStateConsistent(tr *SSETransport) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.connected {
		switch {
		case tr.endpoint == nil:
			return fmt.Errorf("connected without an endpoint")
		case tr.streamBody == nil:
			return fmt.Errorf("connected without a stream body")
		case tr.cancel == nil || tr.streamCtx == nil:
			return fmt.Errorf("connected without an abort handle")
		case tr.streamCtx.Err() != nil:
			return fmt.Errorf("connected on an aborted stream")
		}
	}
	return nil
}

var sseInterleaveGoroutines = []string{"mcp.(*SSETransport).readSSEStream", "mcp.(*SSETransport).Connect("}

// Connect, Close, a superseding Connect, ctx cancel and server EOF in random
// orders: every call returns, nothing leaks, the state stays consistent and
// the transport can connect again. Run with -race -count=50.
func TestSSETransportRandomInterleavings(t *testing.T) {
	seed := time.Now().UnixNano()
	t.Logf("seed %d", seed)
	rng := rand.New(rand.NewSource(seed))
	for iter := 0; iter < 8; iter++ {
		runSSEInterleaving(t, rng.Int63())
		if t.Failed() {
			return
		}
	}
}

func runSSEInterleaving(t *testing.T, seed int64) {
	t.Helper()
	baseline := countGoroutines(sseInterleaveGoroutines...)
	rng := rand.New(rand.NewSource(seed))
	server := newInterleaveSSEServer(seed)
	httpTransport := &http.Transport{DisableKeepAlives: true}
	defer httpTransport.CloseIdleConnections()
	tr := NewSSETransport(SSETransportConfig{URL: server.srv.URL + "/sse", HTTPClient: &http.Client{Transport: httpTransport}})

	var wg sync.WaitGroup
	var cancels []context.CancelFunc
	var ops []string
	connect := func(ctx context.Context) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tr.Connect(ctx)
		}()
	}
	for i, n := 0, 4+rng.Intn(6); i < n; i++ {
		switch rng.Intn(5) {
		case 0, 1:
			ctx, cancel := context.WithCancel(context.Background())
			cancels = append(cancels, cancel)
			connect(ctx)
			ops = append(ops, "connect")
		case 2:
			_ = tr.Close()
			ops = append(ops, "close")
		case 3:
			if len(cancels) > 0 {
				cancels[rng.Intn(len(cancels))]()
				ops = append(ops, "cancel")
			}
		case 4:
			server.endStreams()
			ops = append(ops, "eof")
		}
		if d := rng.Intn(4); d > 0 {
			time.Sleep(time.Duration(d) * time.Millisecond)
		}
	}
	fail := func(format string, args ...any) {
		t.Helper()
		t.Fatalf("seed %d ops %v: %s", seed, ops, fmt.Sprintf(format, args...))
	}

	time.Sleep(10 * time.Millisecond)
	if err := sseStateConsistent(tr); err != nil {
		fail("before Close: %v", err)
	}
	// Connected must mean a live server stream (an adopted closed stream
	// would leave connected set with nothing behind it).
	if !waitFor(2*time.Second, func() bool { return !tr.IsConnected() || server.active.Load() > 0 }) {
		fail("connected with no open server stream")
	}

	_ = tr.Close()
	for _, cancel := range cancels {
		cancel()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		fail("Connect hung after Close and ctx cancel")
	}
	// Another Connect may have started after the Close above; close again
	// now that every Connect has returned.
	_ = tr.Close()
	tr.mu.Lock()
	leftover := tr.connected || tr.streamBody != nil || tr.cancel != nil || tr.streamCtx != nil || tr.endpoint != nil
	tr.mu.Unlock()
	if leftover {
		fail("state left installed after Close")
	}

	// The transport recovers.
	server.mu.Lock()
	server.forced = 0
	server.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := tr.Connect(ctx); err != nil {
		cancel()
		fail("Connect after the interleaving: %v", err)
	}
	if err := tr.Send(ctx, &MCPMessage{JSONRpc: "2.0", Method: "notifications/initialized"}); err != nil {
		cancel()
		fail("Send after reconnecting: %v", err)
	}
	cancel()
	_ = tr.Close()

	if !waitFor(3*time.Second, func() bool { return server.active.Load() == 0 }) {
		fail("%d server streams still open after Close", server.active.Load())
	}
	server.srv.Close()
	if !waitFor(3*time.Second, func() bool { return countGoroutines(sseInterleaveGoroutines...) <= baseline }) {
		fail("transport goroutines leaked: %d, baseline %d", countGoroutines(sseInterleaveGoroutines...), baseline)
	}
}

var httpInterleaveGoroutines = []string{"mcp.(*HTTPTransport)."}

// HTTPTransport: Connect (with an OAuth refresh in flight), Close and a
// second Connect in random orders. Once every call returns, a connected
// transport has a live inbound SSE lifecycle and a closed one has none.
func TestHTTPTransportRandomInterleavings(t *testing.T) {
	seed := time.Now().UnixNano()
	t.Logf("seed %d", seed)
	rng := rand.New(rand.NewSource(seed))
	for iter := 0; iter < 8; iter++ {
		runHTTPInterleaving(t, rng.Int63())
		if t.Failed() {
			return
		}
	}
}

func runHTTPInterleaving(t *testing.T, seed int64) {
	t.Helper()
	baseline := countGoroutines(httpInterleaveGoroutines...)
	rng := rand.New(rand.NewSource(seed))
	var active atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		active.Add(1)
		defer active.Add(-1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	defer srv.Close()
	httpTransport := &http.Transport{DisableKeepAlives: true}
	defer httpTransport.CloseIdleConnections()
	var delayMu sync.Mutex
	delayRng := rand.New(rand.NewSource(seed + 1))
	tr := NewHTTPTransport(HTTPTransportConfig{
		URL:                    srv.URL + "/mcp",
		InitialProtocolVersion: "2025-06-18",
		HTTPClient:             &http.Client{Transport: httpTransport},
		OAuth: &OAuthConfig{RefreshTokenFunc: func(ctx context.Context, _ *OAuthConfig) (string, time.Duration, error) {
			delayMu.Lock()
			d := time.Duration(delayRng.Intn(3)) * time.Millisecond
			delayMu.Unlock()
			time.Sleep(d)
			return "token", time.Hour, nil
		}},
	})

	var wg sync.WaitGroup
	var ops []string
	// seq orders call starts and ends: a Connect that started after every
	// Close had returned, and succeeded, must leave the transport connected.
	var seq atomic.Int64
	var resMu sync.Mutex
	var lastCloseEnd int64
	var okConnectStarts []int64
	for i, n := 0, 3+rng.Intn(6); i < n; i++ {
		if rng.Intn(2) == 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				start := seq.Add(1)
				if tr.Connect(context.Background()) == nil {
					resMu.Lock()
					okConnectStarts = append(okConnectStarts, start)
					resMu.Unlock()
				}
			}()
			ops = append(ops, "connect")
		} else {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = tr.Close()
				end := seq.Add(1)
				resMu.Lock()
				if end > lastCloseEnd {
					lastCloseEnd = end
				}
				resMu.Unlock()
			}()
			ops = append(ops, "close")
		}
		if d := rng.Intn(3); d > 0 {
			time.Sleep(time.Duration(d) * time.Millisecond)
		}
	}
	fail := func(format string, args ...any) {
		t.Helper()
		t.Fatalf("seed %d ops %v: %s", seed, ops, fmt.Sprintf(format, args...))
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		fail("Connect or Close hung")
	}

	tr.mu.Lock()
	connected := tr.connected
	tr.mu.Unlock()
	tr.sseMu.Lock()
	live := tr.sseLifecycleCtx != nil && !tr.sseClosing && tr.sseLifecycleCtx.Err() == nil
	tr.sseMu.Unlock()
	for _, start := range okConnectStarts {
		if start > lastCloseEnd && !connected {
			fail("a Connect that started after every Close returned nil, but the transport is closed")
		}
	}
	if connected != live {
		fail("connected=%v but inbound SSE lifecycle live=%v", connected, live)
	}
	if connected && !waitFor(2*time.Second, func() bool { return active.Load() > 0 }) {
		fail("connected with no inbound SSE stream")
	}

	closed := make(chan struct{})
	go func() { _ = tr.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		fail("final Close hung")
	}
	if tr.IsConnected() {
		fail("connected after Close")
	}
	if !waitFor(3*time.Second, func() bool { return active.Load() == 0 }) {
		fail("%d inbound SSE streams still open after Close", active.Load())
	}
	if err := tr.Connect(context.Background()); err != nil {
		fail("Connect after the interleaving: %v", err)
	}
	_ = tr.Close()
	if !waitFor(3*time.Second, func() bool { return countGoroutines(httpInterleaveGoroutines...) <= baseline }) {
		fail("transport goroutines leaked: %d, baseline %d", countGoroutines(httpInterleaveGoroutines...), baseline)
	}
}
