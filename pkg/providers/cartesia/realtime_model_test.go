package cartesia

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ported from packages/cartesia/src/cartesia-realtime-model.test.ts.

func newTestRealtimeModel(baseURL string) *RealtimeModel {
	p := New(Config{APIKey: "test-key", BaseURL: baseURL, APIVersion: "2026-03-01"})
	m := NewRealtimeModel(p, "ink-2")
	m.now = func() time.Time { return time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC) }
	return m
}

func intp(v int) *int       { return &v }
func strp(v string) *string { return &v }

func TestCartesiaRealtimeClientSecret(t *testing.T) {
	var gotBody map[string]interface{}
	var gotAuth, gotVersion, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotVersion, gotPath = r.Header.Get("Authorization"), r.Header.Get("Cartesia-Version"), r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"token":"access-token"}`))
	}))
	defer srv.Close()
	m := newTestRealtimeModel(srv.URL)
	// The URL scheme follows the base URL (http -> ws); swap to check the shape.
	res, err := m.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{
		ExpiresAfterSeconds: intp(60),
		SessionConfig: &provider.RealtimeSessionConfig{
			InputAudioFormat:        &provider.RealtimeAudioFormat{Type: "audio/pcm", Rate: intp(16000)},
			InputAudioTranscription: &provider.RealtimeAudioTranscriptionConfig{Language: strp("en")},
			TurnDetection:           &provider.RealtimeTurnDetection{Type: "server-vad"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/access-token" || gotAuth != "Bearer test-key" || gotVersion != "2026-03-01" {
		t.Fatalf("request: %s %s %s", gotPath, gotAuth, gotVersion)
	}
	if b, _ := json.Marshal(gotBody); string(b) != `{"expires_in":60,"grants":{"stt":true}}` {
		t.Fatalf("body = %s", b)
	}
	wantURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/stt/turns/websocket?model=ink-2&encoding=pcm_s16le&sample_rate=16000&cartesia_version=2026-03-01"
	if res.Token != "access-token" || res.URL != wantURL || res.ExpiresAt == nil || *res.ExpiresAt != 1783512060 {
		t.Fatalf("result = %+v (want url %s)", res, wantURL)
	}

	res, err = m.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{SessionConfig: &provider.RealtimeSessionConfig{
		InputAudioFormat:        &provider.RealtimeAudioFormat{Type: "audio/pcmu", Rate: intp(8000)},
		InputAudioTranscription: &provider.RealtimeAudioTranscriptionConfig{Language: strp("en")},
		TurnDetection:           &provider.RealtimeTurnDetection{Type: "disabled"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(res.URL, "/stt/websocket?model=ink-2&encoding=pcm_mulaw&sample_rate=8000&cartesia_version=2026-03-01&language=en") {
		t.Fatalf("manual url = %s", res.URL)
	}
}

func TestCartesiaRealtimeClientSecretValidation(t *testing.T) {
	m := newTestRealtimeModel("https://api.cartesia.ai")
	_, err := m.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{SessionConfig: &provider.RealtimeSessionConfig{InputAudioTranscription: &provider.RealtimeAudioTranscriptionConfig{Language: strp("es")}}})
	if err == nil || !strings.Contains(err.Error(), "currently supports English only") {
		t.Fatalf("language: %v", err)
	}
	_, err = m.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{ExpiresAfterSeconds: intp(3601)})
	if err == nil || !strings.Contains(err.Error(), "between 1 and 3600 seconds") {
		t.Fatalf("ttl: %v", err)
	}
}

func TestCartesiaRealtimeWebSocketConfigAndSerialize(t *testing.T) {
	m := newTestRealtimeModel("https://api.cartesia.ai")
	cfg := m.GetWebSocketConfig("access-token", "wss://api.cartesia.ai/stt/turns/websocket?model=ink-2")
	if cfg.URL != "wss://api.cartesia.ai/stt/turns/websocket?model=ink-2&access_token=access-token" {
		t.Fatalf("url = %s", cfg.URL)
	}
	data, binary, handled, err := m.SerializeClientEventRaw(provider.RealtimeClientEvent{Type: "input-audio-append", Audio: "AQID"})
	if err != nil || !handled || !binary || string(data) != "\x01\x02\x03" {
		t.Fatalf("audio: %v %v %v %v", data, binary, handled, err)
	}
	for _, typ := range []string{"input-audio-commit", "session-update"} {
		data, _, handled, err = m.SerializeClientEventRaw(provider.RealtimeClientEvent{Type: typ})
		if err != nil || !handled || len(data) != 0 {
			t.Fatalf("%s should send nothing: %v %v %v", typ, data, handled, err)
		}
	}
	if _, _, _, err = m.SerializeClientEventRaw(provider.RealtimeClientEvent{Type: "conversation-item-truncate"}); err == nil {
		t.Fatal("unsupported event must error")
	}
	m.GetWebSocketConfig("t", "wss://api.cartesia.ai/stt/websocket?model=ink-2")
	data, binary, _, _ = m.SerializeClientEventRaw(provider.RealtimeClientEvent{Type: "input-audio-commit"})
	if string(data) != "finalize" || binary {
		t.Fatalf("manual commit = %q binary=%v", data, binary)
	}
}

func parse(t *testing.T, m *RealtimeModel, raw string) []provider.RealtimeServerEvent {
	t.Helper()
	events, err := m.ParseServerEvent(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestCartesiaRealtimeParseServerEvents(t *testing.T) {
	m := newTestRealtimeModel("https://api.cartesia.ai")
	if e := parse(t, m, `{"type":"connected","request_id":"request-1"}`); len(e) != 1 || e[0].Type != "session-created" || e[0].SessionID != "request-1" {
		t.Fatalf("connected: %+v", e)
	}
	if e := parse(t, m, `{"type":"turn.start","request_id":"request-1"}`); len(e) != 1 || e[0].Type != "speech-started" || e[0].ItemID != "request-1" {
		t.Fatalf("turn.start: %+v", e)
	}
	e := parse(t, m, `{"type":"turn.end","request_id":"request-1","transcript":"Hello world"}`)
	if len(e) != 2 || e[0].Type != "speech-stopped" || e[1].Type != "input-transcription-completed" || e[1].Transcript != "Hello world" {
		t.Fatalf("turn.end: %+v", e)
	}

	// Manual transcript assembly; the first event also announces the session.
	m = newTestRealtimeModel("https://api.cartesia.ai")
	first := `{"type":"transcript","request_id":"request-1","text":"Hello ","is_final":true,"duration":0.5}`
	second := `{"type":"transcript","request_id":"request-1","text":"world","is_final":true,"duration":0.4}`
	e = parse(t, m, first)
	if len(e) != 2 || e[0].Type != "session-created" || e[0].SessionID != "request-1" || e[1].Type != "custom" || e[1].RawType != "transcript" {
		t.Fatalf("first: %+v", e)
	}
	parse(t, m, second)
	e = parse(t, m, `{"type":"flush_done","request_id":"request-1"}`)
	if len(e) != 2 || e[0].Type != "audio-committed" || e[0].ItemID != "request-1" || e[1].Transcript != "Hello world" {
		t.Fatalf("flush: %+v", e)
	}
	var raw struct {
		Event            map[string]interface{}   `json:"event"`
		TranscriptEvents []map[string]interface{} `json:"transcriptEvents"`
		Duration         float64                  `json:"duration"`
	}
	if err := json.Unmarshal(e[1].Raw, &raw); err != nil || len(raw.TranscriptEvents) != 2 || raw.Duration != 0.9 || raw.Event["type"] != "flush_done" {
		t.Fatalf("flush raw: %s %v", e[1].Raw, err)
	}

	m = newTestRealtimeModel("https://api.cartesia.ai")
	e = parse(t, m, `{"type":"done","request_id":"request-1"}`)
	if len(e) != 2 || e[0].Type != "session-created" || e[1].Type != "custom" || e[1].RawType != "done" {
		t.Fatalf("done: %+v", e)
	}

	m = newTestRealtimeModel("https://api.cartesia.ai")
	e = parse(t, m, `{"type":"error","message":"Invalid model","error_code":"model_not_found","request_id":"request-1"}`)
	if len(e) != 2 || e[0].Type != "session-created" || e[1].Type != "error" || e[1].Message != "Invalid model" || e[1].Code != "model_not_found" {
		t.Fatalf("error: %+v", e)
	}
}
