package gateway

import (
	"strings"
	"testing"
)

// TestRealtimeModel_GetWebSocketConfig_TeamFromProviderHeaders verifies the
// x-vercel-ai-gateway-team subprotocol scope comes from the provider's
// resolved header set (gatewayTeamFromHeaders(m.provider.headers)), mirroring
// how transcription_stream.go sources it from the merged per-call headers,
// rather than reading the static TeamIDOrSlug config field directly.
func TestRealtimeModel_GetWebSocketConfig_TeamFromProviderHeaders(t *testing.T) {
	p, err := New(Config{APIKey: "test-token", TeamIDOrSlug: "acme-team"})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	model := p.ExperimentalRealtime("openai/gpt-realtime")

	cfg := model.GetWebSocketConfig("client-secret-token", "wss://example.com/realtime-model")

	wantTeamProtocol := GatewayTeamSubprotocolPrefix + encodeSubprotocolValue("acme-team")
	found := false
	for _, protocol := range cfg.Protocols {
		if protocol == wantTeamProtocol {
			found = true
		}
	}
	if !found {
		t.Fatalf("Protocols = %v, want containing %q", cfg.Protocols, wantTeamProtocol)
	}
}

// TestRealtimeModel_GetWebSocketConfig_NoTeamOmitsProtocol verifies that
// without a configured team, no ai-gateway-team. subprotocol is offered.
func TestRealtimeModel_GetWebSocketConfig_NoTeamOmitsProtocol(t *testing.T) {
	p, err := New(Config{APIKey: "test-token"})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	model := p.ExperimentalRealtime("openai/gpt-realtime")

	cfg := model.GetWebSocketConfig("client-secret-token", "wss://example.com/realtime-model")

	for _, protocol := range cfg.Protocols {
		if strings.HasPrefix(protocol, GatewayTeamSubprotocolPrefix) {
			t.Fatalf("Protocols = %v, want no ai-gateway-team. subprotocol without a configured team", cfg.Protocols)
		}
	}
}
