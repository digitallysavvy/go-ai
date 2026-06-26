package gateway

import "testing"

func TestGatewayRealtimeProtocolsRoundTrip(t *testing.T) {
	protocols := GetGatewayRealtimeProtocols("vcst_token", "team slug/one")
	if len(protocols) != 3 {
		t.Fatalf("protocol count = %d, want 3", len(protocols))
	}
	if protocols[0] != GatewayRealtimeSubprotocol || protocols[1] != "ai-gateway-auth.vcst_token" {
		t.Fatalf("protocols = %#v", protocols)
	}

	header := " ignored, " + protocols[1] + " , " + protocols[2]
	if token := GetGatewayRealtimeAuthToken(header); token != "vcst_token" {
		t.Fatalf("auth token = %q", token)
	}
	if team := GetGatewayRealtimeTeamIDOrSlug(header); team != "team slug/one" {
		t.Fatalf("team = %q", team)
	}
	if got := GetGatewayRealtimeTeamIDOrSlug("ai-gateway-team.not-valid!"); got != "" {
		t.Fatalf("invalid team decode = %q, want empty", got)
	}
}

func TestGatewayRealtimeModelIdentityCodecAndURL(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "https://gateway.example.test/v4/ai", TeamIDOrSlug: "team"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model := p.ExperimentalRealtime("openai/gpt-realtime")
	if model.SpecificationVersion() != "v4" || model.Provider() != "gateway.realtime" || model.ModelID() != "openai/gpt-realtime" {
		t.Fatalf("model metadata mismatch")
	}
	url := ToGatewayRealtimeURL("https://gateway.example.test/v4/ai", "openai/gpt-realtime")
	if url != "wss://gateway.example.test/v4/ai/realtime-model?ai-model-id=openai%2Fgpt-realtime" {
		t.Fatalf("url = %q", url)
	}
	cfg := model.GetWebSocketConfig("vcst_1", url)
	if cfg.URL != url || len(cfg.Protocols) != 3 || cfg.Protocols[1] != "ai-gateway-auth.vcst_1" {
		t.Fatalf("websocket config = %#v", cfg)
	}
	event := map[string]interface{}{"type": "session-update"}
	if got := model.ParseServerEvent(event).(map[string]interface{}); got["type"] != event["type"] {
		t.Fatalf("ParseServerEvent = %#v", got)
	}
	if got := model.SerializeClientEvent(event).(map[string]interface{}); got["type"] != event["type"] {
		t.Fatalf("SerializeClientEvent = %#v", got)
	}
	if got := model.BuildSessionConfig(event).(map[string]interface{}); got["type"] != event["type"] {
		t.Fatalf("BuildSessionConfig = %#v", got)
	}
}
