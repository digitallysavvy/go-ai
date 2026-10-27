package bridgetest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

// handshake records one accepted connection's inbound HTTP request headers,
// in the order connections were accepted (including reconnects/reattaches).
type handshake struct{ headers http.Header }

// Options configures a Server.
type Options struct {
	// Token is the required `agent_bridge_token` query parameter value.
	Token string
	// AnyToken accepts every `agent_bridge_token` value, for tests where the
	// host mints a random token the test cannot know.
	AnyToken bool
	// HelloDelay delays sending bridge-hello after a connection is accepted,
	// for hello-race tests. Zero sends it immediately.
	HelloDelay time.Duration
	// SkipHello never sends bridge-hello, for hello-timeout tests.
	SkipHello bool
	// OnStart is invoked on its own goroutine for every `start` frame with a
	// Turn the test can Emit events on and the decoded frame fields.
	OnStart func(turn *Turn, start map[string]any)
	// OnStop returns the `data` payload for the `bridge-stop` reply to a
	// `stop` frame. Defaults to nil (encoded as `{}`).
	OnStop func() any
	// OnUserMessage answers a `user-message` frame. Defaults to accepting
	// every message.
	OnUserMessage func(messageID, text string) (accepted bool, errMessage string)
	// OnToolResult, when set, receives the decoded body of every inbound
	// `tool-result` frame, so tests can assert what the host forwarded.
	OnToolResult func(frame map[string]any)
	// OnFrame, when set, is called synchronously from the connection's read
	// loop with the type of every inbound frame, before it is dispatched.
	// Unlike OnStart (which runs on its own goroutine) it is ordered with
	// respect to later frames on the same connection, so a test can wait for
	// a later frame and know every earlier one has been observed.
	OnFrame func(frameType string)
}

type entry struct {
	seq  float64
	data []byte
}

// Turn lets a test emit bridge events as the active/attached connection's
// owner, mirroring the TS `BridgeTurn.emit`.
type Turn struct{ s *Server }

// Emit assigns the next seq, appends to the replay log, and best-effort sends
// to the currently active connection (silently dropped if none, or if the
// send fails — mirrors TS `emit`).
func (t *Turn) Emit(frame map[string]any) { t.s.emit(frame) }

// Server is a fake in-process bridge speaking enough of the harness-v1 wire
// protocol to exercise Channel reconnect/replay/resume and Launch against a
// real WebSocket, without the TS runtime.
type Server struct {
	opts Options
	ts   *httptest.Server

	mu         sync.Mutex
	seq        float64
	log        []entry
	active     *serverConn
	state      string // "waiting" | "running"
	handshakes []handshake
}

type serverConn struct {
	ws      *websocket.Conn
	writeMu sync.Mutex
}

// NewServer starts the fake bridge. Call Close when done.
func NewServer(opts Options) *Server {
	s := &Server{opts: opts, state: "waiting"}
	s.ts = httptest.NewServer(websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler:   s.handle,
	})
	return s
}

// Addr returns the fake bridge's host:port.
func (s *Server) Addr() string { return strings.TrimPrefix(s.ts.URL, "http://") }

// URL returns a ws:// endpoint URL carrying the configured token.
func (s *Server) URL() string {
	return "ws://" + s.Addr() + "/?" + bridge.TokenQueryParam + "=" + s.opts.Token
}

// Endpoint returns a harness.PortEndpoint for URL().
func (s *Server) Endpoint() harness.PortEndpoint { return harness.PortEndpoint{URL: s.URL()} }

// Close shuts the fake bridge down.
func (s *Server) Close() { s.ts.Close() }

// HandshakeHeaders returns the inbound HTTP request headers observed on each
// accepted WebSocket connection, in acceptance order (one entry per spawn,
// attach, or reconnect). Unauthorized connections (rejected on token
// mismatch) are not recorded, mirroring the TS test doubles' `webSocketMocks`
// which only capture calls that construct a WebSocket, not server-side
// rejections. Test-only: exists to assert TS's "passes endpoint headers when
// attaching" (deepagents/acp/opencode-harness.test.ts).
func (s *Server) HandshakeHeaders() []http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]http.Header, len(s.handshakes))
	for i, h := range s.handshakes {
		out[i] = h.headers
	}
	return out
}

// DropActive abruptly closes the current active connection (the one that
// last sent `start` or `resume`), simulating a transient network drop. It is
// a no-op if no connection is active.
func (s *Server) DropActive() {
	s.mu.Lock()
	c := s.active
	s.mu.Unlock()
	if c != nil {
		_ = c.ws.Close()
	}
}

func (s *Server) handle(ws *websocket.Conn) {
	token := ws.Request().URL.Query().Get(bridge.TokenQueryParam)
	if !s.opts.AnyToken && token != s.opts.Token {
		// Mirrors runBridge: accept the WebSocket, then close it with code
		// 1008 "unauthorized", rather than rejecting the HTTP upgrade.
		ws.PayloadType = websocket.CloseFrame
		_, _ = ws.Write(append([]byte{0x03, 0xf0}, "unauthorized"...))
		_ = ws.Close()
		return
	}

	c := &serverConn{ws: ws}
	s.mu.Lock()
	lastSeq := s.seq
	state := s.state
	s.handshakes = append(s.handshakes, handshake{headers: ws.Request().Header})
	s.mu.Unlock()

	if !s.opts.SkipHello {
		if s.opts.HelloDelay > 0 {
			time.Sleep(s.opts.HelloDelay)
		}
		supportsUserMessages := true
		_ = c.send(&bridge.Hello{
			State:        state,
			LastSeq:      &lastSeq,
			Capabilities: &bridge.HelloCapabilities{ExperimentalUserMessageResponses: &supportsUserMessages},
		})
	}

	for {
		var raw string
		if err := websocket.Message.Receive(ws, &raw); err != nil {
			s.mu.Lock()
			if s.active == c {
				s.active = nil
			}
			s.mu.Unlock()
			return
		}
		s.handleInbound(c, []byte(raw))
	}
}

func (s *Server) handleInbound(c *serverConn, raw []byte) {
	var probe struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return
	}
	s.mu.Lock()
	onFrame := s.opts.OnFrame
	s.mu.Unlock()
	if onFrame != nil {
		onFrame(probe.Type)
	}
	switch probe.Type {
	case bridge.TypeStart:
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		s.mu.Lock()
		s.active = c
		s.state = "running"
		onStart := s.opts.OnStart
		s.mu.Unlock()
		if onStart != nil {
			go onStart(&Turn{s: s}, payload)
		}
	case bridge.TypeResume:
		var cmd bridge.ResumeCommand
		_ = json.Unmarshal(raw, &cmd)
		s.mu.Lock()
		s.active = c
		s.state = "running"
		var replay []entry
		for _, e := range s.log {
			if e.seq > cmd.LastSeenEventID {
				replay = append(replay, e)
			}
		}
		s.mu.Unlock()
		// Synchronous, so no later live event can slip out ahead of the
		// replayed tail (mirrors TS `resume` handling).
		for _, e := range replay {
			_ = c.sendRaw(e.data)
		}
	case bridge.TypeUserMessage:
		var cmd bridge.UserMessageCommand
		_ = json.Unmarshal(raw, &cmd)
		accepted, errMsg := true, ""
		s.mu.Lock()
		handler := s.opts.OnUserMessage
		s.mu.Unlock()
		if handler != nil {
			accepted, errMsg = handler(cmd.MessageID, cmd.Text)
		}
		if accepted {
			s.emit(map[string]any{
				"type":      bridge.TypeUserMessageResponse,
				"messageId": cmd.MessageID,
				"accepted":  true,
			})
			return
		}
		if errMsg == "" {
			errMsg = "The runtime rejected the user message."
		}
		_ = c.send(&bridge.UserMessageResponse{
			MessageID: cmd.MessageID,
			Accepted:  false,
			Error:     &bridge.UserMessageResponseError{Message: errMsg},
		})
	case bridge.TypeToolResult:
		s.mu.Lock()
		onToolResult := s.opts.OnToolResult
		s.mu.Unlock()
		if onToolResult != nil {
			var payload map[string]any
			_ = json.Unmarshal(raw, &payload)
			onToolResult(payload)
		}
	case bridge.TypeStopCommand:
		s.mu.Lock()
		onStop := s.opts.OnStop
		s.mu.Unlock()
		var data any
		if onStop != nil {
			data = onStop()
		}
		body, _ := json.Marshal(data)
		if len(body) == 0 || string(body) == "null" {
			body = []byte("{}")
		}
		_ = c.send(&bridge.Stop{Data: body})
		_ = c.ws.Close()
	case bridge.TypeDestroyCommand:
		// The real bridge exits after `destroy`, closing the socket.
		_ = c.ws.Close()
	case bridge.TypeAbort:
		// Not needed by current tests; the fake bridge just keeps running.
	}
}

func (s *Server) emit(frame map[string]any) {
	s.mu.Lock()
	s.seq++
	seq := s.seq
	frame["seq"] = seq
	data, err := json.Marshal(frame)
	if err != nil {
		s.mu.Unlock()
		return
	}
	s.log = append(s.log, entry{seq: seq, data: data})
	active := s.active
	s.mu.Unlock()
	if active != nil {
		_ = active.sendRaw(data)
	}
}

func (c *serverConn) send(msg bridge.OutboundMessage) error {
	data, err := bridge.MarshalOutbound(msg)
	if err != nil {
		return err
	}
	return c.sendRaw(data)
}

func (c *serverConn) sendRaw(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return websocket.Message.Send(c.ws, string(data))
}
