package bridge

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"golang.org/x/net/websocket"
)

// Ports of harness/src/utils/sandbox-channel-keepalive.test.ts (TS 04a8f3837e,
// 3334b4d34b). portcarryharHBConn plays the TS HeartbeatSocket; a pong is
// inbound bytes in Go, so the tests raise `bytes` where TS emits `pong`.

type portcarryharHBConn struct {
	*fakeConn
	pings   atomic.Int32
	bytes   atomic.Int64
	pingErr atomic.Pointer[error]
}

func (c *portcarryharHBConn) Ping() error {
	c.pings.Add(1)
	if e := c.pingErr.Load(); e != nil {
		return *e
	}
	return nil
}

func (c *portcarryharHBConn) BytesRead() int64 { return c.bytes.Load() }

func (c *portcarryharHBConn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

type portcarryharHBConnector struct {
	mu    sync.Mutex
	conns []*portcarryharHBConn
}

func (c *portcarryharHBConnector) connect(context.Context) (Conn, error) {
	hc := &portcarryharHBConn{fakeConn: newFakeConn()}
	c.mu.Lock()
	c.conns = append(c.conns, hc)
	c.mu.Unlock()
	return hc, nil
}

func (c *portcarryharHBConnector) at(i int) *portcarryharHBConn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conns[i]
}

func (c *portcarryharHBConnector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.conns)
}

func portcarryharChannel(t *testing.T, keepAlive time.Duration, onDebug func(ChannelDebugEvent)) (*Channel, *portcarryharHBConnector) {
	t.Helper()
	c := &portcarryharHBConnector{}
	ch := NewChannel(ChannelOptions{
		Connect:   c.connect,
		Decode:    decodeFake,
		KeepAlive: keepAlive,
		Reconnect: ReconnectOptions{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxElapsed: 2 * time.Second},
		OnDebug:   onDebug,
	})
	t.Cleanup(ch.Close)
	return ch, c
}

const portcarryharTick = 20 * time.Millisecond

// TS: "terminates after two unanswered pings and starts a fresh heartbeat on reconnect"
func TestPortcarryharKeepAliveTerminatesSilentPeer(t *testing.T) {
	var mu sync.Mutex
	var events []string
	ch, c := portcarryharChannel(t, portcarryharTick, func(e ChannelDebugEvent) {
		mu.Lock()
		events = append(events, e.Event)
		mu.Unlock()
	})
	mustOpen(t, ch)
	first := c.at(0)
	waitFor(t, 2*time.Second, func() bool { return c.count() == 2 })
	if !first.isClosed() {
		t.Fatal("silent socket was not closed")
	}
	if got := first.pings.Load(); got != 2 {
		t.Fatalf("pings on the dead socket = %d, want 2", got)
	}
	waitFor(t, 2*time.Second, func() bool { return c.at(1).pings.Load() >= 1 })
	// The retired socket's heartbeat is gone: no third ping.
	time.Sleep(3 * portcarryharTick)
	if got := first.pings.Load(); got != 2 {
		t.Fatalf("pings after termination = %d, want 2", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) < 2 || events[0] != DebugReconnectAttempt || events[1] != DebugReconnected {
		t.Fatalf("debug events = %v", events)
	}
}

// TS: "keeps a responsive socket connected at the configured interval"
func TestPortcarryharKeepAliveKeepsRespondingPeer(t *testing.T) {
	ch, c := portcarryharChannel(t, portcarryharTick, nil)
	mustOpen(t, ch)
	s := c.at(0)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() { // the pong for each ping
		var seen int32
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Millisecond):
			}
			if n := s.pings.Load(); n != seen {
				seen = n
				s.bytes.Add(2)
			}
		}
	}()
	waitFor(t, 2*time.Second, func() bool { return s.pings.Load() >= 6 })
	if s.isClosed() || c.count() != 1 {
		t.Fatal("a responsive socket was terminated")
	}
}

// TS (3334b4d34b): "does not terminate a socket that is still receiving bytes"
func TestPortcarryharKeepAliveCountsInboundBytes(t *testing.T) {
	ch, c := portcarryharChannel(t, portcarryharTick, nil)
	mustOpen(t, ch)
	s := c.at(0)
	stop := make(chan struct{})
	var stopOnce sync.Once
	t.Cleanup(func() { stopOnce.Do(func() { close(stop) }) })
	go func() { // a large frame still transferring: no pongs, bytes keep arriving
		for {
			select {
			case <-stop:
				return
			case <-time.After(portcarryharTick / 4):
				s.bytes.Add(1024)
			}
		}
	}()
	time.Sleep(10 * portcarryharTick)
	if s.isClosed() || c.count() != 1 {
		t.Fatal("a socket that still receives bytes was terminated")
	}
	// The transfer stalls: the usual two-unanswered-pings deadline applies.
	stopOnce.Do(func() { close(stop) })
	waitFor(t, 2*time.Second, func() bool { return c.count() == 2 })
	if !s.isClosed() {
		t.Fatal("a stalled socket was not terminated")
	}
}

// TS: "disables keepalive when keepAliveMs is zero"
func TestPortcarryharKeepAliveDisabled(t *testing.T) {
	ch, c := portcarryharChannel(t, -1, nil)
	mustOpen(t, ch)
	time.Sleep(5 * portcarryharTick)
	if c.at(0).pings.Load() != 0 || c.at(0).isClosed() {
		t.Fatal("a disabled heartbeat pinged or closed the socket")
	}
}

// TS: "leaves sockets without ping support unchanged"
func TestPortcarryharKeepAliveSkipsConnWithoutPing(t *testing.T) {
	cn := newConnector()
	ch := newTestChannel(t, cn, nil)
	ch.keepAlive = portcarryharTick
	mustOpen(t, ch)
	time.Sleep(5 * portcarryharTick)
	if cn.count() != 1 || cn.current().isClosedForTest() {
		t.Fatal("a connection without ping support was reconnected")
	}
}

func (c *fakeConn) isClosedForTest() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// TS: "cleans up keepalive on close / suspend / beginClose"
func TestPortcarryharKeepAliveStopsOnClose(t *testing.T) {
	for _, method := range []string{"close", "suspend", "beginClose"} {
		t.Run(method, func(t *testing.T) {
			ch, c := portcarryharChannel(t, portcarryharTick, nil)
			mustOpen(t, ch)
			switch method {
			case "close":
				ch.Close()
			case "suspend":
				<-ch.Suspend()
			case "beginClose":
				ch.BeginClose()
			}
			s := c.at(0)
			before := s.pings.Load()
			time.Sleep(6 * portcarryharTick)
			if got := s.pings.Load(); got != before {
				t.Fatalf("pinged %d more times after %s", got-before, method)
			}
			if c.count() != 1 {
				t.Fatalf("reconnected after %s", method)
			}
		})
	}
}

// TS: "cleans up a dropped socket on close / error"
func TestPortcarryharKeepAliveStopsOnDrop(t *testing.T) {
	ch, c := portcarryharChannel(t, portcarryharTick, nil)
	mustOpen(t, ch)
	first := c.at(0)
	first.drop()
	waitFor(t, 2*time.Second, func() bool { return c.count() == 2 })
	waitFor(t, 2*time.Second, func() bool { return c.at(1).pings.Load() >= 1 })
	if first.pings.Load() != 0 {
		t.Fatalf("the dropped socket was pinged %d times", first.pings.Load())
	}
}

// TS: "reconnects if sending a ping throws"
func TestPortcarryharKeepAlivePingErrorReconnects(t *testing.T) {
	ch, c := portcarryharChannel(t, portcarryharTick, nil)
	mustOpen(t, ch)
	first := c.at(0)
	e := errors.New("socket error")
	first.pingErr.Store(&e)
	waitFor(t, 2*time.Second, func() bool { return c.count() == 2 })
	if !first.isClosed() {
		t.Fatal("the socket whose ping failed was not closed")
	}
}

// TS: "does not ping sockets that are no longer open" (a conn that reports
// itself closed is skipped; the reader reports the drop).
type portcarryharClosedHB struct {
	*portcarryharHBConn
}

func (c *portcarryharClosedHB) isOpen() bool { return false }
func (c *portcarryharClosedHB) markClosed()  {}

func TestPortcarryharKeepAliveSkipsClosedConn(t *testing.T) {
	hc := &portcarryharClosedHB{&portcarryharHBConn{fakeConn: newFakeConn()}}
	ch := NewChannel(ChannelOptions{
		Connect:   func(context.Context) (Conn, error) { return hc, nil },
		Decode:    decodeFake,
		KeepAlive: portcarryharTick,
	})
	t.Cleanup(ch.Close)
	mustOpen(t, ch)
	time.Sleep(6 * portcarryharTick)
	if hc.pings.Load() != 0 || hc.isClosed() {
		t.Fatal("a closed socket was pinged or terminated")
	}
}

// The default interval is TS's 20 seconds.
func TestPortcarryharKeepAliveDefault(t *testing.T) {
	if DefaultKeepAlive != 20*time.Second {
		t.Fatalf("DefaultKeepAlive = %v", DefaultKeepAlive)
	}
	if got := NewChannel(ChannelOptions{}).keepAlive; got != DefaultKeepAlive {
		t.Fatalf("zero KeepAlive = %v, want the default", got)
	}
	if got := NewChannel(ChannelOptions{KeepAlive: -1}).keepAlive; got > 0 {
		t.Fatalf("negative KeepAlive = %v, want disabled", got)
	}
}

// The real WebSocket Conn sends a ping that the peer answers, and the pong
// reaches BytesRead (the heartbeat's sign of life).
func TestPortcarryharWSConnPingPongCountsBytes(t *testing.T) {
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		for {
			if _, _, err := wsutil.ReceiveMessage(ws); err != nil { // answers pings
				return
			}
		}
	}))
	defer srv.Close()
	conn, err := Dial(context.Background(), harness.PortEndpoint{URL: "ws" + strings.TrimPrefix(srv.URL, "http")}, DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	hb, ok := conn.(heartbeatConn)
	if !ok {
		t.Fatal("wsConn is not a heartbeatConn")
	}
	go func() { _, _ = conn.Receive() }() // reads the pong
	before := hb.BytesRead()
	if err := hb.Ping(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool { return hb.BytesRead() > before })
}
