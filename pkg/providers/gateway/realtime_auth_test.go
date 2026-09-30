package gateway

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

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

	// ParseServerEvent: the Gateway emits normalized events directly, so
	// decoding must round-trip the fields unchanged (TS "passes server
	// events through unchanged").
	raw := json.RawMessage(`{"type":"response-created","responseId":"resp_1"}`)
	events, err := model.ParseServerEvent(raw)
	if err != nil {
		t.Fatalf("ParseServerEvent error = %v", err)
	}
	if len(events) != 1 || events[0].Type != "response-created" || events[0].ResponseID != "resp_1" {
		t.Fatalf("ParseServerEvent = %#v", events)
	}
	if string(events[0].Raw) != string(raw) {
		t.Fatalf("ParseServerEvent Raw = %s, want the full frame %s", events[0].Raw, raw)
	}

	// SerializeClientEvent: passes normalized client events through as the
	// exact wire shape for that event type (TS "passes client events through
	// unchanged").
	serialized, err := model.SerializeClientEvent(provider.RealtimeClientEvent{Type: "response-cancel"})
	if err != nil {
		t.Fatalf("SerializeClientEvent error = %v", err)
	}
	if string(serialized) != `{"type":"response-cancel"}` {
		t.Fatalf("SerializeClientEvent = %s", serialized)
	}

	// BuildSessionConfig: passes the session config through unchanged (TS
	// "passes session config through unchanged"), including provider options.
	instructions := "be concise"
	sessionConfig := provider.RealtimeSessionConfig{
		Instructions:    &instructions,
		ProviderOptions: map[string]interface{}{"gateway": map[string]interface{}{"tags": []interface{}{"demo"}}},
	}
	built := model.BuildSessionConfig(sessionConfig)
	builtConfig, ok := built.(provider.RealtimeSessionConfig)
	if !ok {
		t.Fatalf("BuildSessionConfig returned %T, want provider.RealtimeSessionConfig", built)
	}
	if builtConfig.Instructions == nil || *builtConfig.Instructions != instructions {
		t.Fatalf("BuildSessionConfig instructions = %#v", builtConfig.Instructions)
	}
	if _, ok := builtConfig.ProviderOptions["gateway"]; !ok {
		t.Fatalf("BuildSessionConfig dropped providerOptions: %#v", builtConfig.ProviderOptions)
	}
}

// TestRealtimeModel_SerializeClientEvent_SessionUpdateAndItems covers the
// remaining event shapes the identity codec must produce verbatim.
func TestRealtimeModel_SerializeClientEvent_SessionUpdateAndItems(t *testing.T) {
	p, err := New(Config{APIKey: "k"})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	model := p.ExperimentalRealtime("openai/gpt-realtime")

	instructions := "be concise"
	raw, err := model.SerializeClientEvent(provider.RealtimeClientEvent{
		Type:   "session-update",
		Config: provider.RealtimeSessionConfig{Instructions: &instructions},
	})
	if err != nil {
		t.Fatalf("SerializeClientEvent(session-update) error = %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	config, ok := decoded["config"].(map[string]interface{})
	if !ok || config["instructions"] != instructions {
		t.Fatalf("session-update config = %#v", decoded)
	}

	name := "get_weather"
	raw, err = model.SerializeClientEvent(provider.RealtimeClientEvent{
		Type: "conversation-item-create",
		Item: provider.RealtimeConversationItem{Type: "function-call-output", CallID: "call_1", Name: &name, Output: `{"ok":true}`},
	})
	if err != nil {
		t.Fatalf("SerializeClientEvent(conversation-item-create) error = %v", err)
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	item, ok := decoded["item"].(map[string]interface{})
	if !ok || item["callId"] != "call_1" || item["name"] != name || item["output"] != `{"ok":true}` {
		t.Fatalf("conversation-item-create item = %#v", decoded)
	}
}
