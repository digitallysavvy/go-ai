package bridge_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

func closedEndpoint(t *testing.T) harness.PortEndpoint {
	t.Helper()
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
	endpoint := srv.Endpoint()
	srv.Close()
	return endpoint
}

// TS #23931: a connection counts as established only after bridge-hello, and
// onHello sees every successful connect.
func TestNewBridgeConnectFunc_WaitsForHello(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", HelloDelay: 50 * time.Millisecond})
	t.Cleanup(srv.Close)
	var hello *bridge.Hello
	connect := bridge.NewBridgeConnectFunc(srv.Endpoint(), bridge.BridgeConnectOptions{
		Name: "test bridge", OnHello: func(h *bridge.Hello) { hello = h },
	})
	start := time.Now()
	conn, err := connect(context.Background())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if hello == nil || !hello.SupportsUserMessageResponses() {
		t.Fatalf("OnHello not called with the hello frame: %+v", hello)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatal("connect returned before the delayed bridge-hello")
	}
}

// TS #23931: only the channel's initial open retries; a reconnect makes a
// single attempt because the channel retries it itself.
func TestNewBridgeConnectFunc_RetriesOnlyTheInitialOpen(t *testing.T) {
	connect := bridge.NewBridgeConnectFunc(closedEndpoint(t), bridge.BridgeConnectOptions{
		Name: "test bridge", OpenRetryTimeout: 700 * time.Millisecond,
	})

	_, err := connect(bridge.WithConnectReason(context.Background(), bridge.ConnectReasonOpen))
	var herr *harness.HarnessError
	if !errors.As(err, &herr) || !strings.Contains(err.Error(), "did not complete the WebSocket handshake within 700ms after") {
		t.Fatalf("initial open err = %v, want the retry-budget error", err)
	}
	if herr.Cause == nil {
		t.Fatal("the last attempt's error must be the cause")
	}
	if strings.Contains(err.Error(), herr.Cause.Error()) {
		t.Fatal("the message must not embed the last error")
	}

	start := time.Now()
	_, err = connect(bridge.WithConnectReason(context.Background(), bridge.ConnectReasonReconnect))
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "did not complete the WebSocket handshake") || time.Since(start) > 400*time.Millisecond {
		t.Fatalf("reconnect retried: %v after %v", err, time.Since(start))
	}
}

// TS #23931: the start context cancels the initial open, not a reconnect.
func TestNewBridgeConnectFunc_StartContextCancelsOnlyTheInitialOpen(t *testing.T) {
	startCtx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("start aborted")
	cancel(cause)
	connect := bridge.NewBridgeConnectFunc(closedEndpoint(t), bridge.BridgeConnectOptions{
		Name: "test bridge", OpenRetryTimeout: 5 * time.Second, StartContext: startCtx,
	})
	start := time.Now()
	_, err := connect(bridge.WithConnectReason(context.Background(), bridge.ConnectReasonOpen))
	if !errors.Is(err, cause) || time.Since(start) > time.Second {
		t.Fatalf("initial open err = %v after %v, want the start abort cause promptly", err, time.Since(start))
	}
	_, err = connect(bridge.WithConnectReason(context.Background(), bridge.ConnectReasonReconnect))
	if errors.Is(err, cause) {
		t.Fatalf("reconnect was cancelled by the start context: %v", err)
	}
}

func TestConnectReasonDefaultsToOpen(t *testing.T) {
	if got := bridge.ConnectReasonFromContext(context.Background()); got != bridge.ConnectReasonOpen {
		t.Fatalf("got %q", got)
	}
}

// TS #23931: the channel tells its connect function whether it is opening or
// reconnecting.
func TestChannelTagsConnectReason(t *testing.T) {
	started := make(chan struct{}, 1)
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(*bridgetest.Turn, map[string]any) { started <- struct{}{} }})
	t.Cleanup(srv.Close)

	var mu sync.Mutex
	var reasons []bridge.ConnectReason
	inner := bridge.NewBridgeConnectFunc(srv.Endpoint(), bridge.BridgeConnectOptions{Name: "test bridge"})
	ch := bridge.NewChannel(bridge.ChannelOptions{
		Connect: func(ctx context.Context) (bridge.Conn, error) {
			mu.Lock()
			reasons = append(reasons, bridge.ConnectReasonFromContext(ctx))
			mu.Unlock()
			return inner(ctx)
		},
		Reconnect: bridge.ReconnectOptions{InitialDelay: 5 * time.Millisecond, MaxDelay: 20 * time.Millisecond, MaxElapsed: 2 * time.Second},
	})
	t.Cleanup(ch.Close)
	if err := ch.Open(context.Background(), false); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := ch.Send(bridge.StartBase{Tools: []harness.ToolSpec{}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	<-started
	srv.DropActive()

	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := len(reasons)
		mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("channel never reconnected")
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if reasons[0] != bridge.ConnectReasonOpen || reasons[1] != bridge.ConnectReasonReconnect {
		t.Fatalf("reasons = %v, want [open reconnect ...]", reasons)
	}
}

// TS bridge-web-socket.test.ts "does not retry when the bridge rejects the
// token": close code 1008 before bridge-hello fails at once with the
// unauthorized error, not after the retry budget.
func TestNewBridgeConnectFunc_BadTokenFailsFast(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
	t.Cleanup(srv.Close)
	endpoint, err := bridge.WithBridgeToken(srv.Endpoint(), "wrong")
	if err != nil {
		t.Fatal(err)
	}
	connect := bridge.NewBridgeConnectFunc(endpoint, bridge.BridgeConnectOptions{
		Name: "test bridge", OpenRetryTimeout: 20 * time.Second,
	})
	start := time.Now()
	_, err = connect(bridge.WithConnectReason(context.Background(), bridge.ConnectReasonOpen))
	want := "The harness bridge rejected the WebSocket connection as unauthorized (code 1008)."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	var unauthorized *bridge.UnauthorizedError
	if !errors.As(err, &unauthorized) || unauthorized.Code != 1008 {
		t.Fatalf("err = %T, want *UnauthorizedError", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took %v, want a fast failure", time.Since(start))
	}
}

// A non-1008 close before hello is still retried and keeps TS's wording.
func TestDial_CloseBeforeHelloReportsCode(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
	endpoint := srv.Endpoint()
	srv.Close()
	_, err := bridge.Dial(context.Background(), endpoint, bridge.DialOptions{WaitForHello: true, HelloTimeout: time.Second})
	if err == nil {
		t.Fatal("expected an error")
	}
	var unauthorized *bridge.UnauthorizedError
	if errors.As(err, &unauthorized) {
		t.Fatalf("a refused connection is not an auth failure: %v", err)
	}
}
