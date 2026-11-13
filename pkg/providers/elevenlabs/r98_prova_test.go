package elevenlabs

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"golang.org/x/net/websocket"
)

// TS JSON.stringify of the realtime client frames keeps the object literal
// key order: message_type, audio_base_64, commit, sample_rate, then
// previous_text (first chunk only); the final commit frame likewise.
func TestR98provaRealtimeFrameKeyOrder(t *testing.T) {
	var mu sync.Mutex
	var frames []string
	srv := httptest.NewServer(websocket.Server{Handler: func(conn *websocket.Conn) {
		_ = websocket.Message.Send(conn, `{"message_type":"session_started","session_id":"s"}`)
		for {
			var raw string
			if err := websocket.Message.Receive(conn, &raw); err != nil {
				return
			}
			mu.Lock()
			frames = append(frames, raw)
			mu.Unlock()
		}
	}})
	defer srv.Close()

	model := newRealtimeTestModel(srv.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}, []byte{2}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		ProviderOptions: map[string]interface{}{"elevenlabs": map[string]interface{}{
			"streaming": map[string]interface{}{"previousText": "p<"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	go func() {
		for {
			if _, err := result.Stream.Next(); err != nil {
				return
			}
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(frames)
		mu.Unlock()
		if n >= 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{
		`{"message_type":"input_audio_chunk","audio_base_64":"AQ==","commit":false,"sample_rate":16000,"previous_text":"p<"}`,
		`{"message_type":"input_audio_chunk","audio_base_64":"Ag==","commit":false,"sample_rate":16000}`,
		`{"message_type":"input_audio_chunk","audio_base_64":"","commit":true,"sample_rate":16000}`,
	}
	if strings.Join(frames, "\n") != strings.Join(want, "\n") {
		t.Fatalf("frames:\n%s\nwant:\n%s", strings.Join(frames, "\n"), strings.Join(want, "\n"))
	}
}

// r98provaRaw waits for n client frames and returns their raw text.
func r98provaRaw(t *testing.T, s *realtimeTestServer, n int) []string {
	t.Helper()
	s.waitForCount(t, n, 2*time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.rawReceived...)
}

// TS JSON.stringify of the speech stream frames keeps the object literal key
// order: the init message (text, voice_settings {stability,
// similarity_boost, style, use_speaker_boost, speed}, generation_config,
// pronunciation_dictionary_locators, then the auth field) and the dialogue
// inputs (text, voice_id, new_turn).
func TestR98provaSpeechFrameKeyOrder(t *testing.T) {
	_, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{
		Text:  speechTexts("Hi"),
		Speed: r98provaFloat(1.1),
		ProviderOptions: map[string]interface{}{"elevenlabs": map[string]interface{}{
			"voiceSettings": map[string]interface{}{"stability": 0.5, "similarityBoost": 0.7, "style": 0.1, "useSpeakerBoost": true},
			"streaming":     map[string]interface{}{"chunkLengthSchedule": []interface{}{50, 90}},
			"pronunciationDictionaryLocators": []interface{}{
				map[string]interface{}{"pronunciationDictionaryId": "d", "versionId": "v"},
			},
		}},
	})
	frames := r98provaRaw(t, server, 1)
	want := `{"text":" ","voice_settings":{"stability":0.5,"similarity_boost":0.7,"style":0.1,"use_speaker_boost":true,"speed":1.1},"generation_config":{"chunk_length_schedule":[50,90]},"pronunciation_dictionary_locators":[{"pronunciation_dictionary_id":"d","version_id":"v"}],"xi-api-key":"test-api-key"}`
	if frames[0] != want {
		t.Fatalf("init frame:\n%s\nwant:\n%s", frames[0], want)
	}

	text := newSpeechText(provider.SpeechStreamTextPart{Text: "Hi ", Voice: "a"}, provider.SpeechStreamTextPart{Text: "there ", Voice: "b"})
	_, server = startSpeech(t, "eleven_v4", &provider.SpeechStreamOptions{
		Text: text, Voice: "a",
		ProviderOptions: map[string]interface{}{"elevenlabs": map[string]interface{}{
			"dialogue": map[string]interface{}{"voices": []interface{}{"a", "b"}},
		}},
	})
	frames = r98provaRaw(t, server, 3)
	var turn string
	for _, f := range frames {
		if strings.Contains(f, `"new_turn"`) {
			turn = f
		}
	}
	if !strings.Contains(turn, `{"text":"there ","voice_id":"b","new_turn":true}`) {
		t.Fatalf("frames = %q, want the new-turn input in TS key order", frames)
	}
}

func r98provaFloat(v float64) *float64 { return &v }

// TS url.searchParams.set(key, String(value)): numbers use the JS number
// text (1e-7, 1e+21), not a fixed-point rendering.
func TestR98provaRealtimeURLNumberText(t *testing.T) {
	small, big := 1e-7, 1e21
	u, err := buildElevenLabsRealtimeURL("https://api.elevenlabs.io", ModelScribeV2Realtime, "pcm_16000", nil,
		&StreamingOptions{VadThreshold: &small, VadSilenceThresholdSecs: &big})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"vad_silence_threshold_secs=1e%2B21", "vad_threshold=1e-7"} {
		if !strings.Contains(u.RawQuery, want) {
			t.Fatalf("query %q, want %s", u.RawQuery, want)
		}
	}
}
