package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestOpenAIRealtimeParseServerEvent(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gpt-realtime")
	tests := []struct {
		raw      string
		wantType string
		check    func(provider.RealtimeServerEvent) bool
	}{
		{`{"type":"response.output_audio.delta","response_id":"r","item_id":"i","delta":"abc"}`, "audio-delta", func(e provider.RealtimeServerEvent) bool { return e.Delta == "abc" }},
		{`{"type":"response.output_text.delta","response_id":"r","item_id":"i","delta":"hello"}`, "text-delta", func(e provider.RealtimeServerEvent) bool { return e.Delta == "hello" }},
		{`{"type":"response.text.delta","response_id":"r","item_id":"i","delta":"hello"}`, "custom", func(e provider.RealtimeServerEvent) bool { return e.RawType == "response.text.delta" }},
		{`{"type":"response.function_call_arguments.delta","response_id":"r","item_id":"i","call_id":"c","delta":"{\"x\""}`, "function-call-arguments-delta", func(e provider.RealtimeServerEvent) bool { return e.CallID == "c" }},
		{`{"type":"response.done","response":{"id":"r","status":"completed"}}`, "response-done", func(e provider.RealtimeServerEvent) bool { return e.ResponseID == "r" && e.Status == "completed" }},
		{`{"type":"error","error":{"message":"bad","code":"invalid"}}`, "error", func(e provider.RealtimeServerEvent) bool { return e.Message == "bad" && e.Code == "invalid" }},
		{`{"type":"provider.new_event","x":1}`, "custom", func(e provider.RealtimeServerEvent) bool {
			return e.RawType == "provider.new_event" && strings.Contains(string(e.Raw), "provider.new_event")
		}},
	}
	for _, tt := range tests {
		events, err := model.ParseServerEvent(json.RawMessage(tt.raw))
		if err != nil {
			t.Fatalf("ParseServerEvent: %v", err)
		}
		if len(events) != 1 || events[0].Type != tt.wantType || !tt.check(events[0]) {
			t.Fatalf("event for %s = %+v", tt.raw, events)
		}
	}
}

func TestOpenAIRealtimeParserPreservesNullishFallbackSemantics(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gpt-realtime")

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

func TestOpenAIRealtimeSerializeClientEvent(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gpt-realtime")
	instructions := "be brief"
	raw, err := model.SerializeClientEvent(provider.RealtimeClientEvent{
		Type: "session-update",
		Config: provider.RealtimeSessionConfig{
			Instructions:     &instructions,
			OutputModalities: []string{"audio"},
			Tools: []provider.RealtimeToolDefinition{{
				Type: "function", Name: "lookup", Parameters: map[string]interface{}{"type": "object"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("SerializeClientEvent session-update: %v", err)
	}
	var got map[string]interface{}
	_ = json.Unmarshal(raw, &got)
	if got["type"] != "session.update" {
		t.Fatalf("type = %v", got["type"])
	}
	session := got["session"].(map[string]interface{})
	if session["model"] != "gpt-realtime" || session["tool_choice"] != "auto" {
		t.Fatalf("session = %#v", session)
	}

	raw, err = model.SerializeClientEvent(provider.RealtimeClientEvent{
		Type: "conversation-item-create",
		Item: provider.RealtimeConversationItem{Type: "text-message", Role: "user", Text: "hi"},
	})
	if err != nil || !strings.Contains(string(raw), `"input_text"`) {
		t.Fatalf("conversation item raw=%s err=%v", raw, err)
	}

	responseInstructions := "go"
	raw, err = model.SerializeClientEvent(provider.RealtimeClientEvent{
		Type:    "response-create",
		Options: &provider.RealtimeResponseCreateOptions{Modalities: []string{"text"}, Instructions: &responseInstructions},
	})
	if err != nil || !strings.Contains(string(raw), `"output_modalities"`) {
		t.Fatalf("response create raw=%s err=%v", raw, err)
	}
}

func TestOpenAIRealtimeSerializeUnsupportedClientEventDropsLikeTypeScript(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gpt-realtime")
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

func TestOpenAIRealtimeSerializesPresentEmptyOptionValues(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gpt-realtime")
	empty := ""
	raw, err := model.SerializeClientEvent(provider.RealtimeClientEvent{
		Type: "response-create",
		Options: &provider.RealtimeResponseCreateOptions{
			Modalities:   []string{},
			Instructions: &empty,
			Metadata:     map[string]interface{}{},
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
	if modalities, ok := response["output_modalities"].([]interface{}); !ok || len(modalities) != 0 {
		t.Fatalf("response = %#v", response)
	}
	if response["instructions"] != "" {
		t.Fatalf("instructions = %#v", response["instructions"])
	}
	if metadata, ok := response["metadata"].(map[string]interface{}); !ok || len(metadata) != 0 {
		t.Fatalf("metadata = %#v", response["metadata"])
	}
}

func TestOpenAIRealtimeCreateClientSecret(t *testing.T) {
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/realtime/client_secrets" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"value": "secret", "expires_at": 123})
	}))
	defer srv.Close()

	model := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1"}), "gpt-realtime")
	ttl := 30
	secret, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{ExpiresAfterSeconds: &ttl})
	if err != nil {
		t.Fatalf("DoCreateClientSecret: %v", err)
	}
	if secret.Token != "secret" || secret.URL != "wss://"+strings.TrimPrefix(srv.URL, "http://")+"/v1/realtime?model=gpt-realtime" {
		t.Fatalf("secret = %+v", secret)
	}
	if captured["expires_after"] == nil {
		t.Fatalf("captured body = %#v", captured)
	}
}

func TestOpenAIRealtimeCreateClientSecretRequiresAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")
	model := NewRealtimeModel(New(Config{}), "gpt-realtime")
	_, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	if err == nil || err.Error() != "OpenAI API key is missing. Pass it using the 'apiKey' parameter or the OPENAI_API_KEY environment variable." {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenAIRealtimeCreateClientSecretForcesJSONContentType(t *testing.T) {
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"value": "secret"})
	}))
	defer srv.Close()

	model := NewRealtimeModel(New(Config{
		APIKey:  "k",
		BaseURL: srv.URL + "/v1",
		Headers: map[string]string{
			"Content-Type": "text/plain",
		},
	}), "gpt-realtime")
	if _, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{}); err != nil {
		t.Fatalf("DoCreateClientSecret: %v", err)
	}
	if contentType != "application/json" {
		t.Fatalf("content-type = %q", contentType)
	}
}

func TestOpenAIRealtimeCreateClientSecretTrimsTrailingBaseURLSlash(t *testing.T) {
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"value": "secret"})
	}))
	defer srv.Close()

	model := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1/"}), "gpt-realtime")
	if _, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{}); err != nil {
		t.Fatalf("DoCreateClientSecret: %v", err)
	}
	if seenPath != "/v1/realtime/client_secrets" {
		t.Fatalf("path = %s", seenPath)
	}
}

func TestOpenAIRealtimeCreateClientSecretUsesEnvSettings(t *testing.T) {
	var auth string
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		seenPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"value": "secret"})
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "env-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

	model := NewRealtimeModel(New(Config{}), "gpt-realtime")
	secret, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	if err != nil {
		t.Fatalf("DoCreateClientSecret: %v", err)
	}
	if auth != "Bearer env-key" {
		t.Fatalf("authorization = %q", auth)
	}
	if seenPath != "/v1/realtime/client_secrets" {
		t.Fatalf("path = %s", seenPath)
	}
	if !strings.HasPrefix(secret.URL, "wss://"+strings.TrimPrefix(srv.URL, "http://")+"/v1/realtime") {
		t.Fatalf("secret url = %s", secret.URL)
	}
}
