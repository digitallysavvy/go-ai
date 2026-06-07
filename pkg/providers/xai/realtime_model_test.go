package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestXAIRealtimeParseSerializeAndUnknown(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "grok-voice-latest")
	events, err := model.ParseServerEvent(json.RawMessage(`{"type":"response.text.delta","response_id":"r","item_id":"i","delta":"hi"}`))
	if err != nil {
		t.Fatalf("ParseServerEvent: %v", err)
	}
	if len(events) != 1 || events[0].Type != "text-delta" || events[0].Delta != "hi" {
		t.Fatalf("events = %+v", events)
	}
	events, err = model.ParseServerEvent(json.RawMessage(`{"type":"mcp_list_tools.completed","tools":[]}`))
	if err != nil {
		t.Fatalf("ParseServerEvent custom: %v", err)
	}
	if events[0].Type != "custom" || events[0].RawType != "mcp_list_tools.completed" || !strings.Contains(string(events[0].Raw), "tools") {
		t.Fatalf("custom event = %+v", events[0])
	}
	raw, err := model.SerializeClientEvent(provider.RealtimeClientEvent{
		Type: "conversation-item-create",
		Item: provider.RealtimeConversationItem{Type: "function-call-output", CallID: "c", Output: `{"ok":true}`},
	})
	if err != nil || !strings.Contains(string(raw), `"function_call_output"`) {
		t.Fatalf("serialized raw=%s err=%v", raw, err)
	}
	raw, err = model.SerializeClientEvent(provider.RealtimeClientEvent{Type: "conversation-item-truncate"})
	if err != nil || raw != nil {
		t.Fatalf("truncate raw=%s err=%v", raw, err)
	}
}

func TestXAIRealtimeParserPreservesNullishFallbackSemantics(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "grok-voice-latest")

	tests := []struct {
		name  string
		raw   string
		check func(provider.RealtimeServerEvent) bool
	}{
		{
			name: "empty nested item id does not fall back",
			raw:  `{"type":"conversation.item.added","item":{"id":""},"item_id":"fallback"}`,
			check: func(e provider.RealtimeServerEvent) bool {
				return e.ItemID == ""
			},
		},
		{
			name: "empty response status does not default",
			raw:  `{"type":"response.done","response":{"id":"","status":""},"response_id":"fallback"}`,
			check: func(e provider.RealtimeServerEvent) bool {
				return e.ResponseID == "" && e.Status == ""
			},
		},
		{
			name: "empty nested error message does not fall back",
			raw:  `{"type":"error","error":{"message":"","code":""},"message":"fallback","code":"fallback"}`,
			check: func(e provider.RealtimeServerEvent) bool {
				return e.Message == "" && e.Code == ""
			},
		},
	}

	for _, tt := range tests {
		events, err := model.ParseServerEvent(json.RawMessage(tt.raw))
		if err != nil {
			t.Fatalf("%s: ParseServerEvent: %v", tt.name, err)
		}
		if len(events) != 1 || !tt.check(events[0]) {
			t.Fatalf("%s: events = %+v", tt.name, events)
		}
	}
}

func TestXAIRealtimeSerializesPresentEmptyOptionValues(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "grok-voice-latest")
	empty := ""
	raw, err := model.SerializeClientEvent(provider.RealtimeClientEvent{
		Type: "response-create",
		Options: &provider.RealtimeResponseCreateOptions{
			Modalities:   []string{},
			Instructions: &empty,
			Metadata:     map[string]interface{}{"ignored": true},
		},
	})
	if err != nil {
		t.Fatalf("SerializeClientEvent: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	response := got["response"].(map[string]interface{})
	if modalities, ok := response["modalities"].([]interface{}); !ok || len(modalities) != 0 {
		t.Fatalf("response = %#v", response)
	}
	if response["instructions"] != "" {
		t.Fatalf("instructions = %#v", response["instructions"])
	}
	if _, ok := response["metadata"]; ok {
		t.Fatalf("xAI response metadata should be ignored, response = %#v", response)
	}
}

func TestXAIRealtimeBuildSessionAppendsProviderOptionToolsFromAnySlice(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "grok-voice-latest")
	session := model.BuildSessionConfig(provider.RealtimeSessionConfig{
		Tools: []provider.RealtimeToolDefinition{{
			Type:       "function",
			Name:       "local",
			Parameters: map[string]interface{}{"type": "object"},
		}},
		ProviderOptions: map[string]interface{}{
			"tools": []map[string]interface{}{
				{"type": "mcp", "server_label": "remote"},
			},
		},
	}).(map[string]interface{})

	tools := session["tools"].([]interface{})
	if len(tools) != 2 {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestXAIRealtimeSerializeUnsupportedClientEventDropsLikeTypeScript(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "grok-voice-latest")
	raw, err := model.SerializeClientEvent(provider.RealtimeClientEvent{Type: "unknown"})
	if err != nil || raw != nil {
		t.Fatalf("unsupported event raw=%s err=%v", raw, err)
	}
	raw, err = model.SerializeClientEvent(provider.RealtimeClientEvent{
		Type: "conversation-item-create",
		Item: provider.RealtimeConversationItem{Type: "unknown"},
	})
	if err != nil || raw != nil {
		t.Fatalf("unsupported item raw=%s err=%v", raw, err)
	}
}

func TestXAIRealtimeCreateClientSecretAndConfig(t *testing.T) {
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/realtime/client_secrets" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"value": "secret", "expires_at": 123})
	}))
	defer srv.Close()

	model := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-voice-latest")
	ttl := 45
	secret, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{ExpiresAfterSeconds: &ttl})
	if err != nil {
		t.Fatalf("DoCreateClientSecret: %v", err)
	}
	if secret.Token != "secret" || !strings.Contains(secret.URL, "/v1/realtime?model=grok-voice-latest") {
		t.Fatalf("secret = %+v", secret)
	}
	if captured["expires_after"] == nil {
		t.Fatalf("body = %#v", captured)
	}
	cfg := model.GetWebSocketConfig(secret.Token, secret.URL)
	if len(cfg.Protocols) != 1 || cfg.Protocols[0] != "xai-client-secret.secret" {
		t.Fatalf("ws config = %+v", cfg)
	}
}

func TestXAIRealtimeCreateClientSecretPreservesTypeScriptCustomBaseURL(t *testing.T) {
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"value": "secret"})
	}))
	defer srv.Close()

	model := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/custom"}), "grok-voice-latest")
	if _, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{}); err != nil {
		t.Fatalf("DoCreateClientSecret: %v", err)
	}
	if seenPath != "/custom/realtime/client_secrets" {
		t.Fatalf("path = %s", seenPath)
	}
}

func TestXAIRealtimeCreateClientSecretRequiresAPIKey(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")
	model := NewRealtimeModel(New(Config{}), "grok-voice-latest")
	_, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	if err == nil || err.Error() != "xAI API key API key is missing. Pass it using the 'apiKey' parameter or the XAI_API_KEY environment variable." {
		t.Fatalf("err = %v", err)
	}
}

func TestXAIRealtimeCreateClientSecretUsesEnvAPIKey(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"value": "secret"})
	}))
	defer srv.Close()
	t.Setenv("XAI_API_KEY", "env-key")

	model := NewRealtimeModel(New(Config{BaseURL: srv.URL}), "grok-voice-latest")
	if _, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{}); err != nil {
		t.Fatalf("DoCreateClientSecret: %v", err)
	}
	if auth != "Bearer env-key" {
		t.Fatalf("authorization = %q", auth)
	}
}

func TestXAIRealtimeCreateClientSecretForcesJSONContentType(t *testing.T) {
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"value": "secret"})
	}))
	defer srv.Close()

	model := NewRealtimeModel(New(Config{
		APIKey:  "k",
		BaseURL: srv.URL,
		Headers: map[string]string{
			"Content-Type": "text/plain",
		},
	}), "grok-voice-latest")
	if _, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{}); err != nil {
		t.Fatalf("DoCreateClientSecret: %v", err)
	}
	if contentType != "application/json" {
		t.Fatalf("content-type = %q", contentType)
	}
}
