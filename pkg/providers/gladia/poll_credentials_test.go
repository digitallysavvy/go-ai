package gladia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The poll URL comes from the provider response: the API key and caller
// headers go only to the configured API origin (TS credentialedOrigin).
func TestPollCredentialsOnlyToConfiguredOrigin(t *testing.T) {
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
}

func TestPollHeadersWithheldFromForeignOrigin(t *testing.T) {
	m := &TranscriptionModel{provider: New(Config{APIKey: "secret", BaseURL: "https://api.gladia.io/v2"}), modelID: "default"}
	own := m.pollHeaders("https://api.gladia.io/v2/pre-recorded/1", "https://api.gladia.io/v2", map[string]string{"X-Caller": "c"})
	if own["x-gladia-key"] != "secret" || own["X-Caller"] != "c" {
		t.Errorf("own origin headers = %v", own)
	}
	for _, u := range []string{"https://evil.example/steal", "http://api.gladia.io/v2/x", "https://api.gladia.io:8443/v2/x", "not a url"} {
		foreign := m.pollHeaders(u, "https://api.gladia.io/v2", map[string]string{"X-Caller": "c"})
		if len(foreign) != 0 {
			t.Errorf("%s got credentials: %v", u, foreign)
		}
	}
}
