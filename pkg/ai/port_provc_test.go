package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// portprovcLiveModel is a continuous model like the TS gateway Live model:
// startup session-start, finalization session-close, client-secret and
// server connections declared as configured.
type portprovcLiveModel struct {
	mockRealtimeModel
	connections []string
	declare     bool
	mints       *int
	mintFn      func(context.Context) (provider.ClientSecretResult, error)
}

func (m portprovcLiveModel) RealtimeConversation() string { return RealtimeConversationContinuous }

func (m portprovcLiveModel) RealtimeLifecycle() provider.RealtimeLifecycle {
	return provider.RealtimeLifecycle{StartupEventType: "session-start", FinalizationEventType: "session-close"}
}

func (m portprovcLiveModel) DoCreateClientSecret(ctx context.Context, _ provider.ClientSecretOptions) (provider.ClientSecretResult, error) {
	if m.mints != nil {
		*m.mints++
	}
	if m.mintFn != nil {
		return m.mintFn(ctx)
	}
	return provider.ClientSecretResult{Token: "vcst_x", URL: "wss://gateway.example/live"}, nil
}

func (m portprovcLiveModel) GetWebSocketConfig(token, url string) provider.WebSocketConfig {
	return provider.WebSocketConfig{URL: url, Protocols: []string{"live.v1", "auth." + token}}
}

func (m portprovcLiveModel) SerializeClientEvent(event provider.RealtimeClientEvent) (json.RawMessage, error) {
	return json.Marshal(map[string]interface{}{"type": event.Type})
}

type portprovcLiveDeclared struct{ portprovcLiveModel }

func (m portprovcLiveDeclared) RealtimeConnections() []string { return m.connections }

func portprovcConnect(t *testing.T, model provider.Experimental_RealtimeModelV4, dialer *mockRealtimeDialer) (*RealtimeSession, error) {
	t.Helper()
	return ConnectRealtime(context.Background(), model, RealtimeSessionOptions{Dialer: dialer})
}

// TS realtime-live-websocket.test.ts 'fetches the token, opens the socket
// with model protocols, and finalizes on close'.
func TestPortProvcContinuousClientSecretConnectsAndFinalizes(t *testing.T) {
	conn := &mockRealtimeConn{}
	dialer := &mockRealtimeDialer{conn: conn}
	model := portprovcLiveDeclared{portprovcLiveModel{connections: []string{"client-secret-websocket", "server-websocket"}}}
	session, err := portprovcConnect(t, model, dialer)
	if err != nil {
		t.Fatalf("ConnectRealtime: %v", err)
	}
	if dialer.config.URL != "wss://gateway.example/live" || strings.Join(dialer.config.Protocols, ",") != "live.v1,auth.vcst_x" {
		t.Fatalf("dial config = %+v", dialer.config)
	}
	if len(conn.sent) != 1 || string(conn.sent[0]) != `{"type":"session-start"}` {
		t.Fatalf("startup frames = %q", conn.sent)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(conn.sent) != 2 || string(conn.sent[1]) != `{"type":"session-close"}` {
		t.Fatalf("finalization frames = %q", conn.sent)
	}
}

// TS 'opens no socket when disconnected during the token request'.
func TestPortProvcNoSocketWhenCancelledDuringTokenRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dialer := &mockRealtimeDialer{conn: &mockRealtimeConn{}}
	model := portprovcLiveDeclared{portprovcLiveModel{
		connections: []string{"client-secret-websocket"},
		mintFn: func(ctx context.Context) (provider.ClientSecretResult, error) {
			cancel()
			<-ctx.Done()
			return provider.ClientSecretResult{}, context.Cause(ctx)
		},
	}}
	if _, err := ConnectRealtime(ctx, model, RealtimeSessionOptions{Dialer: dialer}); err == nil {
		t.Fatal("expected the cancelled token request to fail")
	}
	if dialer.config.URL != "" {
		t.Fatalf("socket dialed: %+v", dialer.config)
	}
}

// TS 'requires an explicit client-secret declaration for continuous token
// sessions' (connections omitted).
func TestPortProvcContinuousRequiresExplicitClientSecretDeclaration(t *testing.T) {
	mints := 0
	dialer := &mockRealtimeDialer{conn: &mockRealtimeConn{}}
	_, err := portprovcConnect(t, portprovcLiveModel{mints: &mints}, dialer)
	if err == nil || err.Error() != "Realtime model does not support client-secret-websocket" {
		t.Fatalf("error = %v", err)
	}
	if mints != 0 || dialer.config.URL != "" {
		t.Fatalf("minted %d secrets / dialed %+v before the check", mints, dialer.config)
	}
}

// TS 'still rejects tokens for continuous models without client-secret
// WebSockets' (connections declared without the kind).
func TestPortProvcContinuousWithoutClientSecretKindRejected(t *testing.T) {
	mints := 0
	model := portprovcLiveDeclared{portprovcLiveModel{connections: []string{"server-websocket"}, mints: &mints}}
	_, err := portprovcConnect(t, model, &mockRealtimeDialer{conn: &mockRealtimeConn{}})
	if err == nil || err.Error() != "Realtime model does not support client-secret-websocket" {
		t.Fatalf("error = %v", err)
	}
	if mints != 0 {
		t.Fatalf("minted %d secrets", mints)
	}
}

// A model that declares connections is checked for the server kind too
// (TS validateConnection, connection = 'server-websocket').
func TestPortProvcServerConnectionMustBeDeclared(t *testing.T) {
	model := portprovcLiveDeclared{portprovcLiveModel{connections: []string{"client-secret-websocket"}}}
	_, err := ConnectRealtime(context.Background(), portprovcServerModel{model}, RealtimeSessionOptions{Dialer: &mockRealtimeDialer{conn: &mockRealtimeConn{}}})
	if err == nil || err.Error() != "Realtime model does not support server-websocket" {
		t.Fatalf("error = %v", err)
	}
}

type portprovcServerModel struct{ portprovcLiveDeclared }

func (portprovcServerModel) GetServerWebSocketConfig() (provider.WebSocketConfig, error) {
	return provider.WebSocketConfig{URL: "wss://server.example"}, nil
}
