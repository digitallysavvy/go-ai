package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
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
	// An empty, non-finished input transcription must not allocate an
	// utterance or emit an empty user message (TS #21549): it falls through
	// to a custom event instead.
	events, err = model.ParseServerEvent(json.RawMessage(`{"inputTranscription":{"text":""}}`))
	if err != nil {
		t.Fatalf("ParseServerEvent empty transcript: %v", err)
	}
	if len(events) != 1 || events[0].Type != "custom" || events[0].RawType != "inputTranscription" {
		t.Fatalf("empty transcript event = %+v", events)
	}
}

// TestGoogleRealtimeInputTranscriptionAccumulatesFragments ports TS
// "accumulates consecutive input transcription fragments into one
// utterance": Google streams input transcription as non-accumulating
// fragments; they must concatenate under one stable synthetic id.
func TestGoogleRealtimeInputTranscriptionAccumulatesFragments(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")

	for _, tt := range []struct {
		text string
		want string
	}{
		{"The quick brown fox", "The quick brown fox"},
		{" jumps over the", "The quick brown fox jumps over the"},
		{" lazy dog.", "The quick brown fox jumps over the lazy dog."},
	} {
		raw := json.RawMessage(`{"serverContent":{"inputTranscription":{"text":` + strconv.Quote(tt.text) + `}}}`)
		events, err := model.ParseServerEvent(raw)
		if err != nil {
			t.Fatalf("ParseServerEvent: %v", err)
		}
		if len(events) != 1 || events[0].Type != "input-transcription-completed" || events[0].ItemID != "google-input-0" || events[0].Transcript != tt.want {
			t.Fatalf("events = %+v, want itemId=google-input-0 transcript=%q", events, tt.want)
		}
	}
}

// TestGoogleRealtimeInputTranscriptionIncrementsAfterTurnComplete ports TS
// "increments input transcription IDs after turnComplete": a new utterance
// after a completed turn gets the next synthetic id.
func TestGoogleRealtimeInputTranscriptionIncrementsAfterTurnComplete(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")

	mustEvents(t, model, `{"serverContent":{"inputTranscription":{"text":"What time is it?"}}}`)
	mustEvents(t, model, `{"serverContent":{"modelTurn":{"parts":[{"inlineData":{"data":"audio1"}}]}}}`)
	mustEvents(t, model, `{"serverContent":{"turnComplete":true}}`)

	events, err := model.ParseServerEvent(json.RawMessage(`{"serverContent":{"inputTranscription":{"text":"And the date?"}}}`))
	if err != nil {
		t.Fatalf("ParseServerEvent: %v", err)
	}
	if len(events) != 1 || events[0].ItemID != "google-input-1" || events[0].Transcript != "And the date?" {
		t.Fatalf("events = %+v, want google-input-1", events)
	}
}

// TestGoogleRealtimeInputTranscriptionSurvivesInterruptionAcrossTurnComplete
// ports TS "keeps an interrupting utterance together across the trailing
// turnComplete": the interrupting utterance must not be split by the
// interrupted response's own trailing turnComplete.
func TestGoogleRealtimeInputTranscriptionSurvivesInterruptionAcrossTurnComplete(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")

	mustEvents(t, model, `{"serverContent":{"inputTranscription":{"text":"Tell me a story."}}}`)
	mustEvents(t, model, `{"serverContent":{"modelTurn":{"parts":[{"inlineData":{"data":"audio1"}}]}}}`)
	mustEvents(t, model, `{"serverContent":{"interrupted":true}}`)

	events := mustEvents(t, model, `{"serverContent":{"inputTranscription":{"text":"Stop","finished":false}}}`)
	if len(events) != 1 || events[0].ItemID != "google-input-1" || events[0].Transcript != "Stop" {
		t.Fatalf("events = %+v, want google-input-1 'Stop'", events)
	}

	mustEvents(t, model, `{"serverContent":{"turnComplete":true}}`)

	events = mustEvents(t, model, `{"serverContent":{"inputTranscription":{"text":" now.","finished":true}}}`)
	if len(events) != 1 || events[0].ItemID != "google-input-1" || events[0].Transcript != "Stop now." {
		t.Fatalf("events = %+v, want google-input-1 'Stop now.' (not split by the interrupted turnComplete)", events)
	}
}

// TestGoogleRealtimeInputTranscriptionStandaloneCompletionMarker ports TS
// "honors a standalone completion marker ... after turnComplete" / "does not
// allocate an utterance for empty input": a `finished` marker with no text
// must not create an empty user message or consume an id.
func TestGoogleRealtimeInputTranscriptionStandaloneCompletionMarker(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")

	for _, raw := range []string{
		`{"serverContent":{"inputTranscription":{}}}`,
		`{"serverContent":{"inputTranscription":{"finished":false}}}`,
		`{"serverContent":{"inputTranscription":{"finished":true}}}`,
		`{"serverContent":{"inputTranscription":{"text":"","finished":true}}}`,
	} {
		events := mustEvents(t, model, raw)
		if len(events) != 1 || events[0].Type != "custom" {
			t.Fatalf("raw=%s events = %+v, want a custom event (no utterance allocated)", raw, events)
		}
	}

	events := mustEvents(t, model, `{"serverContent":{"inputTranscription":{"text":"First"}}}`)
	if len(events) != 1 || events[0].ItemID != "google-input-0" || events[0].Transcript != "First" {
		t.Fatalf("events = %+v, want google-input-0 'First'", events)
	}
}

func mustEvents(t *testing.T, model *GoogleRealtimeModel, raw string) []provider.RealtimeServerEvent {
	t.Helper()
	events, err := model.ParseServerEvent(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("ParseServerEvent(%s): %v", raw, err)
	}
	return events
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
	setup := model.BuildSessionConfig(provider.RealtimeSessionConfig{
		ProviderOptions: map[string]interface{}{
			"generationConfig": map[string]interface{}{"temperature": 0.2},
			"google": map[string]interface{}{
				"translationConfig": map[string]interface{}{
					"targetLanguageCode": "pl",
					"echoTargetLanguage": true,
				},
			},
		},
	}).(map[string]interface{})
	generation := setup["generationConfig"].(map[string]interface{})
	translation := generation["translationConfig"].(map[string]interface{})
	if generation["temperature"] != 0.2 || translation["targetLanguageCode"] != "pl" || translation["echoTargetLanguage"] != true {
		t.Fatalf("generationConfig = %#v", generation)
	}
	if _, ok := setup["google"]; ok {
		t.Fatalf("setup should not include raw google provider options: %#v", setup)
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

// TestGoogleGetRealtimeToken_UsesConcreteModel verifies Provider.GetRealtimeToken
// still mints a client secret after DoCreateClientSecret moved to the
// optional provider.RealtimeClientSecretCreator capability: Google's
// *GoogleRealtimeModel always implements it, so GetRealtimeToken must call
// the concrete model's DoCreateClientSecret directly rather than the general
// provider.Experimental_RealtimeModelV4 interface (hand-off: "realtime
// optional capabilities").
func TestGoogleGetRealtimeToken_UsesConcreteModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "token", "expireTime": "2026-06-07T12:00:00Z"})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})
	secret, err := p.GetRealtimeToken(context.Background(), provider.RealtimeFactoryGetTokenOptions{Model: "gemini-2.0-flash-live-001"})
	if err != nil {
		t.Fatalf("GetRealtimeToken: %v", err)
	}
	if secret.Token != "token" {
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

// TestIsThinkingLiveModel ports TS isThinkingLiveModel
// (google-realtime-event-mapper.ts).
func TestIsThinkingLiveModel(t *testing.T) {
	tests := []struct {
		modelID string
		want    bool
	}{
		{"gemini-3.8-live-extended-thinking", true},
		{"models/gemini-3.8-live-extended-thinking", true},
		{"GEMINI-3.8-LIVE-EXTENDED-THINKING", true},
		{"gemini-2.0-flash-live-001", false},
		{"gemini-3.8-live-preview", false},
	}
	for _, tt := range tests {
		if got := isThinkingLiveModel(tt.modelID); got != tt.want {
			t.Errorf("isThinkingLiveModel(%q) = %v, want %v", tt.modelID, got, tt.want)
		}
	}
}

// TestGoogleRealtimeThinkingConfigDefaultForBackgroundReasoningModel ports
// 4a994ad: a background-reasoning Live model defaults to
// {thinkingLevel: "low"} without any provider options, and an explicit
// providerOptions.google.thinkingConfig overrides the default.
func TestGoogleRealtimeThinkingConfigDefaultForBackgroundReasoningModel(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-3.8-live-extended-thinking")

	setup := model.BuildSessionConfig(provider.RealtimeSessionConfig{}).(map[string]interface{})
	generation := setup["generationConfig"].(map[string]interface{})
	thinkingConfig, ok := generation["thinkingConfig"].(map[string]interface{})
	if !ok || thinkingConfig["thinkingLevel"] != "low" {
		t.Fatalf("thinkingConfig = %#v, want default {thinkingLevel: low}", generation["thinkingConfig"])
	}

	setup = model.BuildSessionConfig(provider.RealtimeSessionConfig{
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{"thinkingBudget": float64(1024)},
			},
		},
	}).(map[string]interface{})
	generation = setup["generationConfig"].(map[string]interface{})
	thinkingConfig = generation["thinkingConfig"].(map[string]interface{})
	if thinkingConfig["thinkingBudget"] != float64(1024) {
		t.Fatalf("explicit thinkingConfig not preserved: %#v", thinkingConfig)
	}
}

// TestGoogleRealtimeNoThinkingConfigForNonBackgroundModel verifies a normal
// Live model gets no thinkingConfig by default.
func TestGoogleRealtimeNoThinkingConfigForNonBackgroundModel(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")
	setup := model.BuildSessionConfig(provider.RealtimeSessionConfig{}).(map[string]interface{})
	generation := setup["generationConfig"].(map[string]interface{})
	if _, ok := generation["thinkingConfig"]; ok {
		t.Fatalf("expected no thinkingConfig, got %#v", generation["thinkingConfig"])
	}
}

// TestGoogleRealtimeThinkingConfigDefaultAddedWhenNeitherLevelNorBudgetSet
// ports TS "adds the default thinkingLevel when thinkingConfig sets neither
// thinkingLevel nor thinkingBudget": a typed thinkingConfig with only
// includeThoughts (or an empty object) still gets the default thinkingLevel
// merged in, keeping its other fields.
func TestGoogleRealtimeThinkingConfigDefaultAddedWhenNeitherLevelNorBudgetSet(t *testing.T) {
	for _, modelID := range []string{"gemini-3.8-live-extended-thinking", "models/gemini-3.8-live-extended-thinking"} {
		model := NewRealtimeModel(New(Config{APIKey: "k"}), modelID)
		setup := model.BuildSessionConfig(provider.RealtimeSessionConfig{
			ProviderOptions: map[string]interface{}{
				"google": map[string]interface{}{
					"thinkingConfig": map[string]interface{}{"includeThoughts": true},
				},
			},
		}).(map[string]interface{})
		generation := setup["generationConfig"].(map[string]interface{})
		thinkingConfig, ok := generation["thinkingConfig"].(map[string]interface{})
		if !ok || thinkingConfig["includeThoughts"] != true || thinkingConfig["thinkingLevel"] != "low" {
			t.Fatalf("modelID=%s thinkingConfig = %#v, want {includeThoughts:true, thinkingLevel:low}", modelID, thinkingConfig)
		}
	}

	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-3.8-live-extended-thinking")
	setup := model.BuildSessionConfig(provider.RealtimeSessionConfig{
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{"thinkingConfig": map[string]interface{}{}},
		},
	}).(map[string]interface{})
	generation := setup["generationConfig"].(map[string]interface{})
	thinkingConfig, ok := generation["thinkingConfig"].(map[string]interface{})
	if !ok || thinkingConfig["thinkingLevel"] != "low" || len(thinkingConfig) != 1 {
		t.Fatalf("empty thinkingConfig = %#v, want {thinkingLevel:low}", thinkingConfig)
	}
}

// TestGoogleRealtimeThinkingBudgetZeroKeptWithoutDefaultLevel ports TS "keeps
// thinkingBudget: 0 without adding a default thinkingLevel": a budget of 0 is
// an explicit value, not absence, so no default thinkingLevel is merged in.
func TestGoogleRealtimeThinkingBudgetZeroKeptWithoutDefaultLevel(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-3.8-live-extended-thinking")
	setup := model.BuildSessionConfig(provider.RealtimeSessionConfig{
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{"thinkingBudget": float64(0)},
			},
		},
	}).(map[string]interface{})
	generation := setup["generationConfig"].(map[string]interface{})
	thinkingConfig, ok := generation["thinkingConfig"].(map[string]interface{})
	if !ok || thinkingConfig["thinkingBudget"] != float64(0) {
		t.Fatalf("thinkingConfig = %#v, want {thinkingBudget: 0}", thinkingConfig)
	}
	if _, has := thinkingConfig["thinkingLevel"]; has {
		t.Fatalf("thinkingLevel should not be added when thinkingBudget is 0: %#v", thinkingConfig)
	}
}

// TestGoogleRealtimeRawThinkingConfigWithLevelOrBudgetUntouched ports TS
// "does not overwrite a raw generationConfig.thinkingConfig that sets
// thinkingLevel or thinkingBudget".
func TestGoogleRealtimeRawThinkingConfigWithLevelOrBudgetUntouched(t *testing.T) {
	cases := []map[string]interface{}{
		{"thinkingLevel": "high"},
		{"thinkingBudget": float64(1024), "includeThoughts": true},
	}
	for _, tc := range cases {
		model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-3.8-live-extended-thinking")
		setup := model.BuildSessionConfig(provider.RealtimeSessionConfig{
			ProviderOptions: map[string]interface{}{
				"generationConfig": map[string]interface{}{
					"responseModalities": []interface{}{"AUDIO"},
					"thinkingConfig":     tc,
				},
			},
		}).(map[string]interface{})
		generation := setup["generationConfig"].(map[string]interface{})
		thinkingConfig, _ := generation["thinkingConfig"].(map[string]interface{})
		if !reflect.DeepEqual(thinkingConfig, tc) {
			t.Fatalf("thinkingConfig = %#v, want untouched %#v", thinkingConfig, tc)
		}
	}
}

// TestGoogleRealtimeRawThinkingConfigGetsDefaultWhenNeitherSet ports TS "adds
// the default thinkingLevel to a raw generationConfig.thinkingConfig that
// sets neither".
func TestGoogleRealtimeRawThinkingConfigGetsDefaultWhenNeitherSet(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-3.8-live-extended-thinking")
	setup := model.BuildSessionConfig(provider.RealtimeSessionConfig{
		ProviderOptions: map[string]interface{}{
			"generationConfig": map[string]interface{}{
				"temperature":    float64(0.2),
				"thinkingConfig": map[string]interface{}{"includeThoughts": true},
			},
		},
	}).(map[string]interface{})
	generation := setup["generationConfig"].(map[string]interface{})
	if generation["temperature"] != float64(0.2) {
		t.Fatalf("temperature = %#v, want 0.2 preserved", generation["temperature"])
	}
	thinkingConfig, ok := generation["thinkingConfig"].(map[string]interface{})
	if !ok || thinkingConfig["includeThoughts"] != true || thinkingConfig["thinkingLevel"] != "low" {
		t.Fatalf("thinkingConfig = %#v, want {includeThoughts:true, thinkingLevel:low}", thinkingConfig)
	}
}

// TestGoogleRealtimeTypedThinkingConfigWinsOverRaw ports TS "prefers a typed
// thinkingConfig over a raw generationConfig.thinkingConfig": the typed
// option replaces the raw one wholesale, even without a level of its own
// (the default is not applied here because the typed path short-circuits).
func TestGoogleRealtimeTypedThinkingConfigWinsOverRaw(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-3.8-live-extended-thinking")
	setup := model.BuildSessionConfig(provider.RealtimeSessionConfig{
		ProviderOptions: map[string]interface{}{
			"generationConfig": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{"thinkingLevel": "high"},
			},
			"google": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{"thinkingBudget": float64(512)},
			},
		},
	}).(map[string]interface{})
	generation := setup["generationConfig"].(map[string]interface{})
	thinkingConfig, ok := generation["thinkingConfig"].(map[string]interface{})
	if !ok || thinkingConfig["thinkingBudget"] != float64(512) || len(thinkingConfig) != 1 {
		t.Fatalf("thinkingConfig = %#v, want {thinkingBudget: 512}", thinkingConfig)
	}
}

// TestGoogleRealtimeNoDefaultThinkingLevelOnNonBackgroundLiveModel ports TS
// "does not add a default thinkingLevel on Live models without background
// reasoning".
func TestGoogleRealtimeNoDefaultThinkingLevelOnNonBackgroundLiveModel(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-3.8-live")
	setup := model.BuildSessionConfig(provider.RealtimeSessionConfig{
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{"includeThoughts": true},
			},
		},
	}).(map[string]interface{})
	generation := setup["generationConfig"].(map[string]interface{})
	thinkingConfig, ok := generation["thinkingConfig"].(map[string]interface{})
	if !ok || thinkingConfig["includeThoughts"] != true || len(thinkingConfig) != 1 {
		t.Fatalf("thinkingConfig = %#v, want {includeThoughts:true} only", thinkingConfig)
	}
}

// TestGoogleRealtimeDefaultToolBehavior ports the defaultToolBehavior half
// of 4a994ad: providerOptions.google.defaultToolBehavior is applied as
// `behavior` on every function declaration.
func TestGoogleRealtimeDefaultToolBehavior(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")
	setup := model.BuildSessionConfig(provider.RealtimeSessionConfig{
		Tools: []provider.RealtimeToolDefinition{{Type: "function", Name: "lookup"}},
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{"defaultToolBehavior": "NON_BLOCKING"},
		},
	}).(map[string]interface{})
	tools := setup["tools"].([]map[string]interface{})
	decls := tools[0]["functionDeclarations"].([]map[string]interface{})
	if decls[0]["behavior"] != "NON_BLOCKING" {
		t.Fatalf("functionDeclarations[0].behavior = %v, want NON_BLOCKING", decls[0]["behavior"])
	}
}

// TestGoogleRealtimeFunctionResponseStructWrapping ports d93e295:
// functionResponse.response must always be an object. An object output
// passes through unchanged; a non-object (or malformed JSON) output is
// wrapped under an "output" key.
func TestGoogleRealtimeFunctionResponseStructWrapping(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")

	t.Run("object output passes through", func(t *testing.T) {
		raw, err := model.SerializeClientEvent(provider.RealtimeClientEvent{
			Type: "conversation-item-create",
			Item: provider.RealtimeConversationItem{
				Type: "function-call-output", CallID: "call-1", Output: `{"temp": 72}`,
			},
		})
		if err != nil {
			t.Fatalf("SerializeClientEvent: %v", err)
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		response := decoded["toolResponse"].(map[string]interface{})["functionResponses"].([]interface{})[0].(map[string]interface{})["response"].(map[string]interface{})
		if response["temp"] != float64(72) {
			t.Fatalf("response = %#v, want the object passed through unchanged", response)
		}
	})

	t.Run("string output is wrapped", func(t *testing.T) {
		raw, err := model.SerializeClientEvent(provider.RealtimeClientEvent{
			Type: "conversation-item-create",
			Item: provider.RealtimeConversationItem{
				Type: "function-call-output", CallID: "call-2", Output: `"sunny"`,
			},
		})
		if err != nil {
			t.Fatalf("SerializeClientEvent: %v", err)
		}
		var decoded map[string]interface{}
		_ = json.Unmarshal(raw, &decoded)
		response := decoded["toolResponse"].(map[string]interface{})["functionResponses"].([]interface{})[0].(map[string]interface{})["response"].(map[string]interface{})
		if response["output"] != "sunny" {
			t.Fatalf("response = %#v, want {output: \"sunny\"}", response)
		}
	})

	t.Run("array output is wrapped", func(t *testing.T) {
		raw, err := model.SerializeClientEvent(provider.RealtimeClientEvent{
			Type: "conversation-item-create",
			Item: provider.RealtimeConversationItem{
				Type: "function-call-output", CallID: "call-3", Output: `[1,2,3]`,
			},
		})
		if err != nil {
			t.Fatalf("SerializeClientEvent: %v", err)
		}
		var decoded map[string]interface{}
		_ = json.Unmarshal(raw, &decoded)
		response := decoded["toolResponse"].(map[string]interface{})["functionResponses"].([]interface{})[0].(map[string]interface{})["response"].(map[string]interface{})
		output, ok := response["output"].([]interface{})
		if !ok || len(output) != 3 {
			t.Fatalf("response = %#v, want {output: [1,2,3]}", response)
		}
	})

	t.Run("malformed JSON output preserved as raw string, wrapped", func(t *testing.T) {
		raw, err := model.SerializeClientEvent(provider.RealtimeClientEvent{
			Type: "conversation-item-create",
			Item: provider.RealtimeConversationItem{
				Type: "function-call-output", CallID: "call-4", Output: `not json`,
			},
		})
		if err != nil {
			t.Fatalf("SerializeClientEvent: %v", err)
		}
		var decoded map[string]interface{}
		_ = json.Unmarshal(raw, &decoded)
		response := decoded["toolResponse"].(map[string]interface{})["functionResponses"].([]interface{})[0].(map[string]interface{})["response"].(map[string]interface{})
		if response["output"] != "not json" {
			t.Fatalf("response = %#v, want {output: \"not json\"} preserving the raw text", response)
		}
	})
}

// TestGoogleRealtimeLifecycleCustomEvents ports 66b7151 (goAway,
// sessionResumptionUpdate, generationComplete) and the interactionStatus/
// waitingForInput half of 4a994ad: each surfaces as a `custom` event with
// the matching rawType.
func TestGoogleRealtimeLifecycleCustomEvents(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")

	assertCustom := func(t *testing.T, payload, wantRawType string) {
		t.Helper()
		events, err := model.ParseServerEvent(json.RawMessage(payload))
		if err != nil {
			t.Fatalf("ParseServerEvent: %v", err)
		}
		found := false
		for _, e := range events {
			if e.Type == "custom" && e.RawType == wantRawType {
				found = true
			}
		}
		if !found {
			t.Fatalf("events = %+v, want a custom event with rawType %q", events, wantRawType)
		}
	}

	t.Run("goAway", func(t *testing.T) {
		assertCustom(t, `{"goAway":{"timeLeft":"10s"}}`, "goAway")
	})
	t.Run("sessionResumptionUpdate", func(t *testing.T) {
		assertCustom(t, `{"sessionResumptionUpdate":{"newHandle":"abc","resumable":true}}`, "sessionResumptionUpdate")
	})
	t.Run("generationComplete", func(t *testing.T) {
		assertCustom(t, `{"serverContent":{"generationComplete":true}}`, "generationComplete")
	})
	t.Run("interactionStatus", func(t *testing.T) {
		assertCustom(t, `{"serverContent":{"interactionStatus":"WAITING_FOR_INPUT"}}`, "interactionStatus")
	})
	t.Run("waitingForInput", func(t *testing.T) {
		assertCustom(t, `{"serverContent":{"waitingForInput":true}}`, "waitingForInput")
	})
}
