package gateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"golang.org/x/net/websocket"
)

// TestGatewayRealtimeModel_ConnectRealtime is a regression test for the gap
// found in review: gateway.RealtimeModel did not implement
// provider.Experimental_RealtimeModelV4 (mismatched method signatures using
// bespoke gateway.RealtimeClientSecretOptions/Result/WebSocketConfig types
// instead of the shared provider ones), so it could not be driven through
// ai.ConnectRealtime the way the OpenAI/Google/xAI realtime models can. This
// exercises the full path: ConnectRealtime dials the Gateway realtime
// WebSocket subprotocol handshake, sends the initial session-update built
// from RealtimeModel.BuildSessionConfig, and reads back a normalized server
// event through RealtimeModel.ParseServerEvent.
func TestGatewayRealtimeModel_ConnectRealtime(t *testing.T) {
	var gotProtocolHeader string
	clientEvents := make(chan string, 1)

	wsHandler := websocket.Server{
		Handshake: func(cfg *websocket.Config, r *http.Request) error {
			gotProtocolHeader = r.Header.Get("Sec-WebSocket-Protocol")
			// AcceptHandshake requires exactly one protocol to be echoed
			// back when more than one was offered.
			if len(cfg.Protocol) > 0 {
				cfg.Protocol = cfg.Protocol[:1]
			}
			return nil
		},
		Handler: func(conn *websocket.Conn) {
			defer conn.Close() //nolint:errcheck

			var msg string
			if err := websocket.Message.Receive(conn, &msg); err != nil {
				return
			}
			clientEvents <- msg

			if err := websocket.Message.Send(conn, `{"type":"response-created","responseId":"resp_1"}`); err != nil {
				return
			}
		},
	}
	ts := httptest.NewServer(wsHandler)
	defer ts.Close()

	wsURL := "ws" + ts.URL[len("http"):]

	p, err := gateway.New(gateway.Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("gateway.New() error = %v", err)
	}
	model := p.ExperimentalRealtime("openai/gpt-realtime")

	// Verify the model satisfies the shared realtime interface at the call
	// site, exactly like ai.ConnectRealtime requires.
	var _ provider.Experimental_RealtimeModelV4 = model

	instructions := "be concise"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := ai.ConnectRealtime(ctx, model, ai.RealtimeSessionOptions{
		ClientSecret:  &provider.ClientSecretResult{Token: "vcst_test", URL: wsURL},
		SessionConfig: &provider.RealtimeSessionConfig{Instructions: &instructions},
	})
	if err != nil {
		t.Fatalf("ai.ConnectRealtime() error = %v", err)
	}
	defer session.Close() //nolint:errcheck

	if !strings.Contains(gotProtocolHeader, gateway.GatewayRealtimeSubprotocol) ||
		!strings.Contains(gotProtocolHeader, gateway.GatewayAuthSubprotocolPrefix+"vcst_test") {
		t.Fatalf("Sec-WebSocket-Protocol = %q, want it to contain the marker and auth subprotocols", gotProtocolHeader)
	}

	select {
	case sent := <-clientEvents:
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(sent), &decoded); err != nil {
			t.Fatalf("client sent invalid JSON %q: %v", sent, err)
		}
		if decoded["type"] != "session-update" {
			t.Fatalf("client's initial event type = %v, want session-update", decoded["type"])
		}
		config, ok := decoded["config"].(map[string]interface{})
		if !ok || config["instructions"] != instructions {
			t.Fatalf("session-update config = %#v, want instructions %q", decoded["config"], instructions)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the initial session-update")
	}

	events, err := session.Read(ctx)
	if err != nil {
		t.Fatalf("session.Read() error = %v", err)
	}
	if len(events) != 1 || events[0].Type != "response-created" || events[0].ResponseID != "resp_1" {
		t.Fatalf("events = %+v, want one response-created event with responseId resp_1", events)
	}
}
