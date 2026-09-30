package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// bareRealtimeModel implements only the required
// provider.Experimental_RealtimeModelV4 methods, mirroring TS
// OpenAIRealtimeModelLive, which implements neither the optional
// doCreateClientSecret nor getWebSocketConfig. Both are now optional
// capabilities (RealtimeClientSecretCreator / RealtimeWebSocketConfigProvider)
// rather than required interface methods (hand-off: "realtime optional
// capabilities").
type bareRealtimeModel struct{}

func (bareRealtimeModel) SpecificationVersion() string { return "v4" }
func (bareRealtimeModel) Provider() string             { return "bare.realtime" }
func (bareRealtimeModel) ModelID() string              { return "bare-model" }
func (bareRealtimeModel) BuildSessionConfig(config provider.RealtimeSessionConfig) any {
	return config
}
func (bareRealtimeModel) ParseServerEvent(raw json.RawMessage) ([]provider.RealtimeServerEvent, error) {
	return nil, nil
}
func (bareRealtimeModel) SerializeClientEvent(event provider.RealtimeClientEvent) (json.RawMessage, error) {
	return nil, nil
}

var (
	_ provider.Experimental_RealtimeModelV4 = bareRealtimeModel{}
)

func TestConnectRealtime_MissingClientSecretCreatorCapability(t *testing.T) {
	_, err := ConnectRealtime(context.Background(), bareRealtimeModel{}, RealtimeSessionOptions{
		Dialer: &mockRealtimeDialer{},
	})
	if err == nil {
		t.Fatal("expected error when model lacks RealtimeClientSecretCreator and no ClientSecret was supplied")
	}
	if !strings.Contains(err.Error(), "does not support minting a client secret") {
		t.Fatalf("error = %v, want a RealtimeClientSecretCreator message", err)
	}
}

func TestConnectRealtime_MissingWebSocketConfigProviderCapability(t *testing.T) {
	_, err := ConnectRealtime(context.Background(), bareRealtimeModel{}, RealtimeSessionOptions{
		Dialer:       &mockRealtimeDialer{},
		ClientSecret: &provider.ClientSecretResult{Token: "t", URL: "wss://example.test"},
	})
	if err == nil {
		t.Fatal("expected error when model lacks RealtimeWebSocketConfigProvider even with a supplied ClientSecret")
	}
	// Mirrors TS realtime-session.ts validateConnection()'s message for a
	// missing getWebSocketConfig when a client-secret token is in play.
	if err.Error() != "Realtime model does not support client-secret WebSocket configuration" {
		t.Fatalf("error = %v", err)
	}
}
