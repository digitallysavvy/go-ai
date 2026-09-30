package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"golang.org/x/net/websocket"
)

type RealtimeWebSocketConn interface {
	Send(ctx context.Context, message []byte) error
	Receive(ctx context.Context) ([]byte, error)
	Close() error
}

// RealtimeBinaryConn is an optional RealtimeWebSocketConn capability for a
// transport that can distinguish and send/receive binary WS frames
// (WebSocket has always had text/binary opcodes; this surfaces that
// distinction to RealtimeSession). A conn that doesn't implement it is
// treated exactly as before: every Send is a text frame and Receive's
// result is treated as text (audit row 8f89c25 / WG-MISC).
type RealtimeBinaryConn interface {
	SendBinary(ctx context.Context, message []byte) error
	// ReceiveFrame returns the frame payload and whether it arrived as a
	// binary frame.
	ReceiveFrame(ctx context.Context) (data []byte, binary bool, err error)
}

type RealtimeDialer interface {
	Dial(ctx context.Context, config provider.WebSocketConfig) (RealtimeWebSocketConn, error)
}

type RealtimeSessionOptions struct {
	ClientSecret  *provider.ClientSecretResult
	SessionConfig *provider.RealtimeSessionConfig
	Dialer        RealtimeDialer
}

type RealtimeSession struct {
	model     provider.Experimental_RealtimeModelV4
	conn      RealtimeWebSocketConn
	mu        sync.Mutex
	done      chan struct{}
	parser    func(raw json.RawMessage) ([]provider.RealtimeServerEvent, error)
	lifecycle provider.RealtimeLifecycle
}

func ConnectRealtime(ctx context.Context, model provider.Experimental_RealtimeModelV4, opts RealtimeSessionOptions) (*RealtimeSession, error) {
	if model == nil {
		return nil, errors.New("realtime model is required")
	}
	dialer := opts.Dialer
	if dialer == nil {
		dialer = WebSocketRealtimeDialer{}
	}

	// A server-websocket-config model (e.g. OpenAI Live) authenticates the
	// connection itself with request headers instead of a per-connection
	// token minted via DoCreateClientSecret, matching TS
	// getServerWebSocketConfig().
	var cfg provider.WebSocketConfig
	if serverModel, ok := model.(provider.RealtimeServerWebSocketConfigProvider); ok {
		built, err := serverModel.GetServerWebSocketConfig()
		if err != nil {
			return nil, err
		}
		cfg = built
	} else {
		secret := opts.ClientSecret
		if secret == nil {
			creator, ok := model.(provider.RealtimeClientSecretCreator)
			if !ok {
				return nil, errors.New("realtime model does not support minting a client secret (RealtimeClientSecretCreator); pass ClientSecret explicitly instead")
			}
			created, err := creator.DoCreateClientSecret(ctx, provider.ClientSecretOptions{SessionConfig: opts.SessionConfig})
			if err != nil {
				return nil, err
			}
			secret = &created
		}
		wsConfigProvider, ok := model.(provider.RealtimeWebSocketConfigProvider)
		if !ok {
			return nil, errors.New("Realtime model does not support client-secret WebSocket configuration")
		}
		cfg = wsConfigProvider.GetWebSocketConfig(secret.Token, secret.URL)
	}

	conn, err := dialer.Dial(ctx, cfg)
	if err != nil {
		return nil, err
	}

	s := &RealtimeSession{model: model, conn: conn, done: make(chan struct{})}
	if factory, ok := model.(provider.RealtimeServerEventParserFactory); ok {
		s.parser = factory.NewServerEventParser()
	}
	if lp, ok := model.(provider.RealtimeLifecycleProvider); ok {
		s.lifecycle = lp.RealtimeLifecycle()
	}

	startupType := s.lifecycle.StartupEventType
	if startupType == "" {
		startupType = "session-update"
	}
	sessionConfig := opts.SessionConfig
	// A non-default startup event (e.g. OpenAI Live's "session-start")
	// starts the session itself, so it must always be sent, even with an
	// empty config; the default "session-update" is only sent when the
	// caller supplied a config.
	if sessionConfig != nil || s.lifecycle.StartupEventType != "" {
		cfgValue := provider.RealtimeSessionConfig{}
		if sessionConfig != nil {
			cfgValue = *sessionConfig
		}
		if err := s.Send(ctx, provider.RealtimeClientEvent{Type: startupType, Config: cfgValue}); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()
	return s, nil
}

func (s *RealtimeSession) Send(ctx context.Context, event provider.RealtimeClientEvent) error {
	if s == nil || s.conn == nil {
		return errors.New("realtime session is not connected")
	}
	// A model may want to bypass the default JSON-object serialization for
	// this event (e.g. a raw string payload sent as-is, or binary audio
	// sent as a binary WS frame) — mirrors TS encodeRealtimeFrame (audit
	// row 8f89c25 / WG-MISC).
	if rawSerializer, ok := s.model.(provider.RealtimeRawEventSerializer); ok {
		data, binary, handled, err := rawSerializer.SerializeClientEventRaw(event)
		if err != nil {
			return err
		}
		if handled {
			if len(data) == 0 {
				return nil
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if binary {
				if binConn, ok := s.conn.(RealtimeBinaryConn); ok {
					return binConn.SendBinary(ctx, data)
				}
			}
			return s.conn.Send(ctx, data)
		}
	}
	raw, err := s.model.SerializeClientEvent(event)
	if err != nil {
		return err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.Send(ctx, raw)
}

func (s *RealtimeSession) Read(ctx context.Context) ([]provider.RealtimeServerEvent, error) {
	if s == nil || s.conn == nil {
		return nil, errors.New("realtime session is not connected")
	}
	rawParser, hasRawParser := s.model.(provider.RealtimeRawEventParser)
	for {
		var rawBytes []byte
		var binary bool
		var err error
		if binConn, ok := s.conn.(RealtimeBinaryConn); ok {
			rawBytes, binary, err = binConn.ReceiveFrame(ctx)
		} else {
			rawBytes, err = s.conn.Receive(ctx)
		}
		if err != nil {
			return nil, err
		}
		// A model that opts in via RealtimeRawEventParser receives every
		// frame — binary or text, valid JSON or not — instead of core
		// silently dropping anything that isn't valid JSON (audit row
		// 8f89c25 / WG-MISC: TS passes binary server frames straight to
		// the model's parser).
		if hasRawParser {
			// The health-check responder is still consulted for a text
			// frame that happens to be valid JSON, matching
			// RealtimeHealthCheckResponder's contract of running "before
			// parseServerEvent" regardless of which parser path is active.
			// A binary frame, or text that isn't JSON, can't carry a
			// health-check ping, so it's skipped.
			if !binary {
				if raw := json.RawMessage(rawBytes); json.Valid(raw) {
					if checker, ok := s.model.(provider.RealtimeHealthCheckResponder); ok {
						response, ok := checker.GetHealthCheckResponse(raw)
						if ok && len(response) > 0 && string(response) != "null" {
							if err := s.conn.Send(ctx, response); err != nil {
								return nil, err
							}
						}
					}
				}
			}
			events, err := rawParser.ParseRawServerEvent(append([]byte(nil), rawBytes...), binary)
			if err != nil {
				return nil, err
			}
			if len(events) == 0 {
				continue
			}
			return events, nil
		}
		raw := json.RawMessage(append([]byte(nil), rawBytes...))
		if !json.Valid(raw) {
			continue
		}
		if checker, ok := s.model.(provider.RealtimeHealthCheckResponder); ok {
			response, ok := checker.GetHealthCheckResponse(raw)
			if ok && len(response) > 0 && string(response) != "null" {
				if err := s.conn.Send(ctx, response); err != nil {
					return nil, err
				}
			}
		}
		if s.parser != nil {
			return s.parser(raw)
		}
		return s.model.ParseServerEvent(raw)
	}
}

func (s *RealtimeSession) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	select {
	case <-s.done:
		return nil
	default:
		close(s.done)
	}
	if s.lifecycle.FinalizationEventType != "" {
		// Best-effort: a graceful finalization event (e.g. OpenAI Live's
		// "session-close") lets the provider end the session cleanly. A
		// failure here must not prevent the socket from closing.
		_ = s.Send(context.Background(), provider.RealtimeClientEvent{Type: s.lifecycle.FinalizationEventType})
	}
	return s.conn.Close()
}

// WebSocketRealtimeDialer is the default RealtimeDialer, backed by
// golang.org/x/net/websocket. Origin is unused: dial derives the handshake's
// Origin header from the target URL itself (x/net/websocket requires one;
// TS's connectToWebSocket, dialing through a browser or Node WebSocket
// client, sets none). Kept as a field for backward compatibility with
// existing callers; a future release may remove it.
type WebSocketRealtimeDialer struct {
	Origin string
}

func (d WebSocketRealtimeDialer) Dial(ctx context.Context, config provider.WebSocketConfig) (RealtimeWebSocketConn, error) {
	conn, err := wsutil.Dial(ctx, config.URL, wsutil.DialOptions{Protocols: config.Protocols, Headers: config.Headers})
	if err != nil {
		return nil, err
	}
	return &xNetWebSocketConn{conn: conn}, nil
}

type xNetWebSocketConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *xNetWebSocketConn) Send(ctx context.Context, message []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return wsutil.Send(ctx, c.conn, string(message))
}

func (c *xNetWebSocketConn) Receive(ctx context.Context) ([]byte, error) {
	text, err := wsutil.Receive(ctx, c.conn)
	if err != nil {
		if wsutil.IsCleanClose(err) {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("realtime websocket receive failed: %w", err)
	}
	return []byte(text), nil
}

func (c *xNetWebSocketConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// SendBinary sends message as a binary WS frame (RealtimeBinaryConn), unlike
// Send which always sends a text frame. golang.org/x/net/websocket's Message
// codec dispatches on the Go type given to Send: a string is a TextFrame, a
// []byte is a BinaryFrame (see its marshal function).
func (c *xNetWebSocketConn) SendBinary(ctx context.Context, message []byte) error {
	done := make(chan error, 1)
	c.mu.Lock()
	go func() {
		defer c.mu.Unlock()
		done <- websocket.Message.Send(c.conn, message)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// realtimeFrameTypeCodec mirrors websocket.Message but additionally reports
// the frame's payload type. websocket.Message discards it: its Unmarshal
// only inspects the target's Go type (*string/*[]byte), never the frame's
// actual TextFrame/BinaryFrame opcode, so there is no way to learn whether a
// received frame was binary through the public Codec API.
var realtimeFrameTypeCodec = websocket.Codec{
	Marshal: func(v interface{}) (msg []byte, payloadType byte, err error) {
		switch data := v.(type) {
		case string:
			return []byte(data), websocket.TextFrame, nil
		case []byte:
			return data, websocket.BinaryFrame, nil
		}
		return nil, websocket.UnknownFrame, fmt.Errorf("realtime: unsupported send type %T", v)
	},
	Unmarshal: func(msg []byte, payloadType byte, v interface{}) error {
		out, ok := v.(*realtimeFrame)
		if !ok {
			return fmt.Errorf("realtime: unsupported receive type %T", v)
		}
		out.data = append([]byte(nil), msg...)
		out.binary = payloadType == websocket.BinaryFrame
		return nil
	},
}

type realtimeFrame struct {
	data   []byte
	binary bool
}

// ReceiveFrame implements RealtimeBinaryConn, reporting whether the received
// frame was a binary (vs. text) WS frame.
func (c *xNetWebSocketConn) ReceiveFrame(ctx context.Context) ([]byte, bool, error) {
	type result struct {
		frame realtimeFrame
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		var frame realtimeFrame
		err := realtimeFrameTypeCodec.Receive(c.conn, &frame)
		ch <- result{frame: frame, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case res := <-ch:
		if res.err != nil {
			if errors.Is(res.err, io.EOF) {
				return nil, false, io.EOF
			}
			return nil, false, fmt.Errorf("realtime websocket receive failed: %w", res.err)
		}
		return res.frame.data, res.frame.binary, nil
	}
}
