package gladia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TS `?? undefined` keeps an empty string, so these free-form options are sent as "".
func TestR33_EmptyStringOptionsAreSent(t *testing.T) {
	provOpts := map[string]interface{}{"gladia": map[string]interface{}{
		"contextPrompt": "", "language": "",
		"translationConfig": map[string]interface{}{"targetLanguages": []interface{}{"fr"}, "model": ""},
	}}
	body := buildRequestBody(extractOptions(provOpts))
	keepEmptyStringOptions(body, provOpts)
	for _, k := range []string{"context_prompt", "language"} {
		if v, ok := body[k]; !ok || v != "" {
			t.Errorf("%s = %v (present %v)", k, v, ok)
		}
	}
	tc, _ := body["translation_config"].(map[string]interface{})
	if v, ok := tc["model"]; !ok || v != "" {
		t.Errorf("translation_config.model = %v", tc)
	}
}

// The poll URL comes from the provider response: the API key and caller
// headers go only to the configured API origin (TS credentialedOrigin).
func TestR33_PollCredentialsOnlyToConfiguredOrigin(t *testing.T) {
	var gotKey, gotCaller string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotCaller = r.Header.Get("x-gladia-key"), r.Header.Get("X-Caller")
		_, _ = w.Write([]byte(`{"status":"done","result":{"metadata":{"audio_duration":1},"transcription":{"full_transcript":"t","languages":[],"utterances":[]}}}`))
	}))
	defer srv.Close()
	m := &TranscriptionModel{provider: New(Config{APIKey: "secret", BaseURL: srv.URL}), modelID: "default"}
	if _, _, err := m.pollResult(context.Background(), srv.URL+"/result", map[string]string{"X-Caller": "c"}); err != nil {
		t.Fatal(err)
	}
	if gotKey != "secret" || gotCaller != "c" {
		t.Fatalf("same-origin poll lost credentials: %q %q", gotKey, gotCaller)
	}
	// A foreign origin never receives them (the request is blocked as a
	// loopback target here, before any header could be sent).
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-gladia-key")
	}))
	defer other.Close()
	gotKey = ""
	_, _, _ = m.pollResult(context.Background(), other.URL+"/result", nil)
	if gotKey != "" {
		t.Fatalf("foreign origin received the API key")
	}
}

func TestR33_PollHeadersWithheldFromForeignOrigin(t *testing.T) {
	m := &TranscriptionModel{provider: New(Config{APIKey: "secret", BaseURL: "https://api.gladia.io/v2"}), modelID: "default"}
	own := m.pollHeaders("https://api.gladia.io/v2/pre-recorded/1", "https://api.gladia.io/v2", map[string]string{"X-Caller": "c"})
	if own["x-gladia-key"] != "secret" || own["x-caller"] != "c" {
		t.Errorf("own origin headers = %v", own)
	}
	foreign := m.pollHeaders("https://evil.example/steal", "https://api.gladia.io/v2", map[string]string{"X-Caller": "c"})
	if _, ok := foreign["x-gladia-key"]; ok || foreign["x-caller"] != "" {
		t.Errorf("foreign origin got credentials: %v", foreign)
	}
}
