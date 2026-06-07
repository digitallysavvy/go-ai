package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"golang.org/x/net/websocket"
)

type RealtimeWebSocketConn interface {
	Send(ctx context.Context, message []byte) error
	Receive(ctx context.Context) ([]byte, error)
	Close() error
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
	model provider.Experimental_RealtimeModelV4
	conn  RealtimeWebSocketConn
	mu    sync.Mutex
	done  chan struct{}
}

func ConnectRealtime(ctx context.Context, model provider.Experimental_RealtimeModelV4, opts RealtimeSessionOptions) (*RealtimeSession, error) {
	if model == nil {
		return nil, errors.New("realtime model is required")
	}
	secret := opts.ClientSecret
	if secret == nil {
		created, err := model.DoCreateClientSecret(ctx, provider.ClientSecretOptions{SessionConfig: opts.SessionConfig})
		if err != nil {
			return nil, err
		}
		secret = &created
	}
	dialer := opts.Dialer
	if dialer == nil {
		dialer = WebSocketRealtimeDialer{}
	}
	cfg := model.GetWebSocketConfig(secret.Token, secret.URL)
	conn, err := dialer.Dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s := &RealtimeSession{model: model, conn: conn, done: make(chan struct{})}
	if opts.SessionConfig != nil {
		if err := s.Send(ctx, provider.RealtimeClientEvent{Type: "session-update", Config: *opts.SessionConfig}); err != nil {
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
	for {
		rawBytes, err := s.conn.Receive(ctx)
		if err != nil {
			return nil, err
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
	return s.conn.Close()
}

type WebSocketRealtimeDialer struct {
	Origin string
}

func (d WebSocketRealtimeDialer) Dial(ctx context.Context, config provider.WebSocketConfig) (RealtimeWebSocketConn, error) {
	origin := d.Origin
	if origin == "" {
		origin = "http://localhost/"
	}
	wsConfig, err := websocket.NewConfig(config.URL, origin)
	if err != nil {
		return nil, err
	}
	wsConfig.Protocol = append([]string(nil), config.Protocols...)
	type result struct {
		conn *websocket.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := websocket.DialConfig(wsConfig)
		ch <- result{conn: conn, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		return &xNetWebSocketConn{conn: res.conn}, nil
	}
}

type xNetWebSocketConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *xNetWebSocketConn) Send(ctx context.Context, message []byte) error {
	done := make(chan error, 1)
	c.mu.Lock()
	go func() {
		defer c.mu.Unlock()
		done <- websocket.Message.Send(c.conn, string(message))
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func (c *xNetWebSocketConn) Receive(ctx context.Context) ([]byte, error) {
	type result struct {
		msg string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		var msg string
		err := websocket.Message.Receive(c.conn, &msg)
		ch <- result{msg: msg, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.err != nil {
			if errors.Is(res.err, io.EOF) {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("realtime websocket receive failed: %w", res.err)
		}
		return []byte(res.msg), nil
	}
}

func (c *xNetWebSocketConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}
