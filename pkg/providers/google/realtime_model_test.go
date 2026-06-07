package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestGoogleRealtimeParseSingleMessageToMultipleEvents(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")
	events, err := model.ParseServerEvent(json.RawMessage(`{
		"serverContent": {
			"modelTurn": {"parts": [{"inlineData": {"data": "audio"}}, {"text": "hello"}]},
			"outputTranscription": {"text": "spoken"},
			"turnComplete": true
		}
	}`))
	if err != nil {
		t.Fatalf("ParseServerEvent: %v", err)
	}
	want := []string{"audio-delta", "text-delta", "audio-transcript-delta", "audio-done", "text-done", "audio-transcript-done", "response-done"}
	if len(events) != len(want) {
		t.Fatalf("events = %+v", events)
	}
	for i, typ := range want {
		if events[i].Type != typ {
			t.Fatalf("events[%d].Type = %q, want %q", i, events[i].Type, typ)
		}
	}
	if events[0].ResponseID != "google-resp-0" || events[0].ItemID != "google-item-0" {
		t.Fatalf("synthetic ids = %+v", events[0])
	}
}

func TestGoogleRealtimeUnknownAndHealthCheck(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")
	events, err := model.ParseServerEvent(json.RawMessage(`{"mystery":{}}`))
	if err != nil {
		t.Fatalf("ParseServerEvent: %v", err)
	}
	if len(events) != 1 || events[0].Type != "custom" || events[0].RawType != "mystery" {
		t.Fatalf("events = %+v", events)
	}
	events, err = model.ParseServerEvent(json.RawMessage(`{"first":{},"second":{}}`))
	if err != nil {
		t.Fatalf("ParseServerEvent first key: %v", err)
	}
	if len(events) != 1 || events[0].RawType != "first" {
		t.Fatalf("first-key custom event = %+v", events)
	}
	events, err = model.ParseServerEvent(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("ParseServerEvent empty: %v", err)
	}
	if len(events) != 1 || events[0].Type != "custom" || events[0].RawType != "undefined" {
		t.Fatalf("empty custom event = %+v", events)
	}
	events, err = model.ParseServerEvent(json.RawMessage(`{"setupComplete":null}`))
	if err != nil {
		t.Fatalf("ParseServerEvent null setup: %v", err)
	}
	if len(events) != 1 || events[0].Type != "custom" || events[0].RawType != "setupComplete" {
		t.Fatalf("null setup event = %+v", events)
	}
	events, err = model.ParseServerEvent(json.RawMessage(`{"inputTranscription":{"text":""}}`))
	if err != nil {
		t.Fatalf("ParseServerEvent empty transcript: %v", err)
	}
	if len(events) != 1 || events[0].Type != "input-transcription-completed" || events[0].Transcript != "" {
		t.Fatalf("empty transcript event = %+v", events)
	}
}

func TestGoogleRealtimeBuildSessionAndSerialize(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")
	rate := 24000
	voice := "Puck"
	raw, err := model.SerializeClientEvent(provider.RealtimeClientEvent{
		Type: "session-update",
		Config: provider.RealtimeSessionConfig{
			Voice:            &voice,
			OutputModalities: []string{"text", "audio"},
			InputAudioFormat: &provider.RealtimeAudioFormat{Type: "audio/pcm", Rate: &rate},
			Tools: []provider.RealtimeToolDefinition{{
				Type: "function", Name: "lookup", Parameters: map[string]interface{}{"type": "object", "$schema": "http://json-schema.org/draft-07/schema#"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("SerializeClientEvent: %v", err)
	}
	if !strings.Contains(string(raw), `"responseModalities":["TEXT","AUDIO"]`) || strings.Contains(string(raw), `"$schema"`) {
		t.Fatalf("session raw = %s", raw)
	}
	raw, err = model.SerializeClientEvent(provider.RealtimeClientEvent{Type: "input-audio-append", Audio: "abc"})
	if err != nil || !strings.Contains(string(raw), `audio/pcm;rate=24000`) {
		t.Fatalf("audio append raw=%s err=%v", raw, err)
	}
}

func TestGoogleRealtimeSerializeUnsupportedClientEventReturnsNullLikeTypeScript(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")
	raw, err := model.SerializeClientEvent(provider.RealtimeClientEvent{Type: "unknown"})
	if err != nil || string(raw) != "null" {
		t.Fatalf("unsupported event raw=%s err=%v", raw, err)
	}
	raw, err = model.SerializeClientEvent(provider.RealtimeClientEvent{
		Type: "conversation-item-create",
		Item: provider.RealtimeConversationItem{Type: "unknown"},
	})
	if err != nil || string(raw) != "null" {
		t.Fatalf("unsupported item raw=%s err=%v", raw, err)
	}
}

func TestGoogleRealtimeToolSchemaConversionMatchesTypeScript(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")
	session := model.BuildSessionConfig(provider.RealtimeSessionConfig{
		Tools: []provider.RealtimeToolDefinition{
			{
				Type:       "function",
				Name:       "empty",
				Parameters: map[string]interface{}{"type": "object", "$schema": "http://json-schema.org/draft-07/schema#"},
			},
			{
				Type: "function",
				Name: "nullable",
				Parameters: map[string]interface{}{
					"type":     "object",
					"required": []interface{}{"value"},
					"properties": map[string]interface{}{
						"value": map[string]interface{}{
							"anyOf": []map[string]interface{}{
								{"type": "string"},
								{"type": "null"},
							},
						},
						"nested": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					},
				},
			},
		},
	}).(map[string]interface{})

	tools := session["tools"].([]map[string]interface{})
	decls := tools[0]["functionDeclarations"].([]map[string]interface{})
	if _, ok := decls[0]["parameters"]; ok {
		t.Fatalf("empty root parameters should be omitted, decl = %#v", decls[0])
	}
	params := decls[1]["parameters"].(map[string]interface{})
	props := params["properties"].(map[string]interface{})
	value := props["value"].(map[string]interface{})
	if value["nullable"] != true || value["type"] != "string" {
		t.Fatalf("nullable property = %#v", value)
	}
	nested := props["nested"].(map[string]interface{})
	if nested["type"] != "object" {
		t.Fatalf("nested empty object = %#v", nested)
	}
}

func TestGoogleRealtimeParseEmptyToolCallMatchesTypeScript(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")
	events, err := model.ParseServerEvent(json.RawMessage(`{"toolCall":{"functionCalls":[]}}`))
	if err != nil {
		t.Fatalf("ParseServerEvent: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %+v", events)
	}
}

func TestGoogleRealtimeCreateClientSecret(t *testing.T) {
	var path string
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if r.URL.Query().Get("key") != "k" {
			t.Fatalf("key query = %q", r.URL.Query().Get("key"))
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "token", "expireTime": "2026-06-07T12:00:00Z"})
	}))
	defer srv.Close()

	model := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"}), "gemini-2.0-flash-live-001")
	secret, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	if err != nil {
		t.Fatalf("DoCreateClientSecret: %v", err)
	}
	if path != "/v1alpha/auth_tokens" {
		t.Fatalf("path = %s", path)
	}
	if body["uses"].(float64) != 0 || body["bidiGenerateContentSetup"] == nil {
		t.Fatalf("body = %#v", body)
	}
	if expireTime, _ := body["expireTime"].(string); !hasMillisecondsZulu(expireTime) {
		t.Fatalf("expireTime = %q", expireTime)
	}
	if newSessionExpireTime, _ := body["newSessionExpireTime"].(string); !hasMillisecondsZulu(newSessionExpireTime) {
		t.Fatalf("newSessionExpireTime = %q", newSessionExpireTime)
	}
	if secret.Token != "token" || !strings.Contains(secret.URL, "/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContentConstrained") {
		t.Fatalf("secret = %+v", secret)
	}
}

func TestGoogleRealtimeCreateClientSecretUsesEffectiveHeaderAPIKey(t *testing.T) {
	var queryKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryKey = r.URL.Query().Get("key")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "token"})
	}))
	defer srv.Close()

	model := NewRealtimeModel(New(Config{
		APIKey:  "config-key",
		BaseURL: srv.URL + "/v1beta",
		Headers: map[string]string{
			"x-goog-api-key": "header-key",
		},
	}), "gemini-2.0-flash-live-001")
	if _, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{}); err != nil {
		t.Fatalf("DoCreateClientSecret: %v", err)
	}
	if queryKey != "header-key" {
		t.Fatalf("query key = %q", queryKey)
	}
}

func TestGoogleRealtimeCreateClientSecretTrimsTrailingBaseURLSlash(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "token"})
	}))
	defer srv.Close()

	model := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta/"}), "gemini-2.0-flash-live-001")
	secret, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	if err != nil {
		t.Fatalf("DoCreateClientSecret: %v", err)
	}
	if path != "/v1alpha/auth_tokens" {
		t.Fatalf("path = %s", path)
	}
	if !strings.Contains(secret.URL, "/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContentConstrained") || strings.Contains(secret.URL, "/v1beta/ws/") {
		t.Fatalf("secret url = %s", secret.URL)
	}
}

func hasMillisecondsZulu(value string) bool {
	if len(value) < len(".000Z") || value[len(value)-1] != 'Z' || value[len(value)-5] != '.' {
		return false
	}
	for _, c := range value[len(value)-4 : len(value)-1] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func TestGoogleRealtimeGetWebSocketConfigMatchesTypeScript(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")
	config := model.GetWebSocketConfig("my-token", "wss://example.com/ws")
	if config.URL != "wss://example.com/ws?access_token=my-token" {
		t.Fatalf("url = %q", config.URL)
	}
	if len(config.Protocols) != 0 {
		t.Fatalf("protocols = %#v", config.Protocols)
	}
}
