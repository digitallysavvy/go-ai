package elevenlabs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// elevenlabs-speech-model.test.ts (TS 3e5a12334b): outputFormat "wav" is
// sent as ElevenLabs' wav_44100 for speech, dialogue, music and sound
// effects.
func TestR85provaElevenLabsWavOutputFormat(t *testing.T) {
	if got := mapElevenLabsOutputFormat("wav"); got != "wav_44100" {
		t.Fatalf("wav -> %q", got)
	}
	if got := speechOutputFormat("wav"); got != "wav_44100" {
		t.Fatalf("speechOutputFormat(wav) = %q", got)
	}
	if got := mapElevenLabsOutputFormat("wav_22050"); got != "wav_22050" {
		t.Fatalf("native format changed: %q", got)
	}
}

func r85provaMultipart(json *string, audio []byte, reverse, closeIt bool) []byte {
	var parts [][]byte
	if json != nil {
		parts = append(parts, []byte("--music-boundary\r\nContent-Type: application/json\r\n\r\n"+*json+"\r\n"))
	}
	if audio != nil {
		p := []byte("--music-boundary\r\nContent-Type: audio/mpeg\r\nContent-Disposition: attachment; filename=\"music.mp3\"\r\n\r\n")
		p = append(p, audio...)
		p = append(p, '\r', '\n')
		parts = append(parts, p)
	}
	if reverse {
		parts[0], parts[len(parts)-1] = parts[len(parts)-1], parts[0]
	}
	if closeIt {
		parts = append(parts, []byte("--music-boundary--\r\n"))
	}
	return bytes.Join(parts, nil)
}

// elevenlabs-music-response.test.ts (TS 48ab95d62a).
func TestR85provaElevenLabsMusicResponse(t *testing.T) {
	audio := append(append([]byte{255, 0, 128, 13, 10}, []byte("--music-boundary-extra")...), 0, 255, 13, 10)
	meta := `{"words_timestamps":[{"word":"Hello 🌍","start_ms":120,"end_ms":945}]}`
	ct := "multipart/mixed; boundary=music-boundary"
	for _, reverse := range []bool{false, true} {
		got, err := parseMusicTimestampsResponse(ct, r85provaMultipart(&meta, audio, reverse, true))
		if err != nil || !bytes.Equal(got.audio, audio) || !got.hasWords || len(got.wordTimestamps) != 1 ||
			got.wordTimestamps[0] != (MusicWordTimestamp{Word: "Hello 🌍", StartMs: 120, EndMs: 945}) {
			t.Fatalf("reverse=%v: %+v, %v", reverse, got, err)
		}
	}
	if got, err := parseMusicTimestampsResponse(`Multipart/Mixed; boundary="music-boundary"; charset=utf-8`, r85provaMultipart(&meta, audio, false, true)); err != nil || !bytes.Equal(got.audio, audio) {
		t.Fatalf("quoted boundary: %v", err)
	}
	for m, wantWords := range map[string]bool{`{}`: false, `{"words_timestamps":null}`: false, `{"words_timestamps":[]}`: true} {
		m := m
		got, err := parseMusicTimestampsResponse(ct, r85provaMultipart(&m, audio, false, true))
		if err != nil || got.hasWords != wantWords || (wantWords && len(got.wordTimestamps) != 0) {
			t.Fatalf("%s: %+v %v", m, got, err)
		}
	}
	if got, err := parseMusicTimestampsResponse(ct, r85provaMultipart(&meta, []byte{}, false, true)); err != nil || got.audio == nil || len(got.audio) != 0 {
		t.Fatalf("empty audio: %+v %v", got, err)
	}
	for name, body := range map[string][]byte{
		"metadata":         r85provaMultipart(nil, audio, false, true),
		"audio":            r85provaMultipart(&meta, nil, false, true),
		"closing boundary": r85provaMultipart(&meta, audio, false, false),
	} {
		if _, err := parseMusicTimestampsResponse(ct, body); err == nil || !strings.Contains(err.Error(), "complete metadata and audio parts") {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	for _, bad := range []string{"application/json", "multipart/mixed", `multipart/mixed; boundary="wrong"`} {
		if _, err := parseMusicTimestampsResponse(bad, r85provaMultipart(&meta, audio, false, true)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	for _, bad := range []string{`{`, `{"words_timestamps":[{"word":"Hello","start_ms":"invalid","end_ms":1}]}`} {
		bad := bad
		if _, err := parseMusicTimestampsResponse(ct, r85provaMultipart(&bad, audio, false, true)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

type r85provaCall struct {
	path  string
	query string
	body  map[string]interface{}
}

func r85provaServer(t *testing.T, contentType string, respond []byte) (*Provider, *[]r85provaCall) {
	t.Helper()
	var calls []r85provaCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, r85provaCall{path: r.URL.EscapedPath(), query: r.URL.RawQuery, body: body})
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(respond)
	}))
	t.Cleanup(srv.Close)
	return New(Config{APIKey: "k", BaseURL: srv.URL}), &calls
}

// elevenlabs-speech-model.test.ts (TS 48ab95d62a): withTimestamps,
// eleven_v4 through Text to Dialogue, and the encoded voice path.
func TestR85provaElevenLabsSpeechTimestamps(t *testing.T) {
	ts := []byte(`{"audio_base64":"AQID","alignment":{"characters":["H","i"],"character_start_times_seconds":[0,0.1],"character_end_times_seconds":[0.1,0.2]},"normalized_alignment":null,"voice_segments":[{"voice_id":"v","start_time_seconds":0,"end_time_seconds":0.2,"character_start_index":0,"character_end_index":2,"dialogue_input_index":0}]}`)
	withTS := map[string]interface{}{"elevenlabs": map[string]interface{}{"withTimestamps": true}}

	p, calls := r85provaServer(t, "application/json", ts)
	res, err := NewSpeechModel(p, "eleven_multilingual_v2").DoGenerate(t.Context(), &provider.SpeechGenerateOptions{Text: "Hi", Voice: "a/b c", ProviderOptions: withTS})
	if err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0].path; got != "/v1/text-to-speech/a%2Fb%20c/with-timestamps" {
		t.Errorf("path = %s", got)
	}
	meta := res.ProviderMetadata["elevenlabs"].(map[string]interface{})
	if !bytes.Equal(res.Audio, []byte{1, 2, 3}) || meta["alignment"] == nil || meta["normalizedAlignment"] != nil || meta["voiceSegments"] == nil {
		t.Errorf("result = %v %v", res.Audio, meta)
	}

	p, calls = r85provaServer(t, "application/json", ts)
	res, err = NewSpeechModel(p, "eleven_v4").DoGenerate(t.Context(), &provider.SpeechGenerateOptions{Text: "Hello", Voice: "voice-1", ProviderOptions: withTS})
	if err != nil {
		t.Fatal(err)
	}
	c := (*calls)[0]
	inputs, _ := c.body["inputs"].([]interface{})
	if c.path != "/v1/text-to-dialogue/with-timestamps" || len(inputs) != 1 || fmt.Sprint(inputs[0]) != "map[text:Hello voice_id:voice-1]" || len(res.Warnings) != 0 {
		t.Errorf("v4: %+v warnings %v", c, res.Warnings)
	}

	p, calls = r85provaServer(t, "audio/mpeg", []byte{9})
	res, err = NewSpeechModel(p, "eleven_v4").DoGenerate(t.Context(), &provider.SpeechGenerateOptions{Text: "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	inputs, _ = (*calls)[0].body["inputs"].([]interface{})
	if (*calls)[0].path != "/v1/text-to-dialogue" || fmt.Sprint(inputs) != "[map[text:Hello voice_id:21m00Tcm4TlvDq8ikWAM]]" || !bytes.Equal(res.Audio, []byte{9}) {
		t.Errorf("v4 default voice: %+v", (*calls)[0])
	}

	music := r85provaMultipart(&[]string{`{"words_timestamps":[{"word":"la","start_ms":1,"end_ms":2}]}`}[0], []byte{7, 8}, false, true)
	p, calls = r85provaServer(t, "multipart/mixed; boundary=music-boundary", music)
	res, err = NewSpeechModel(p, SpeechModelMusicV2).DoGenerate(t.Context(), &provider.SpeechGenerateOptions{Text: "a song", ProviderOptions: withTS})
	if err != nil {
		t.Fatal(err)
	}
	meta = res.ProviderMetadata["elevenlabs"].(map[string]interface{})
	if (*calls)[0].path != "/v1/music/detailed" || (*calls)[0].body["with_timestamps"] != true || !bytes.Equal(res.Audio, []byte{7, 8}) || meta["wordTimestamps"] == nil {
		t.Errorf("music: %+v %v", (*calls)[0], meta)
	}

	p, calls = r85provaServer(t, "audio/mpeg", []byte{1})
	res, err = NewSpeechModel(p, SpeechModelTextToSoundV2).DoGenerate(t.Context(), &provider.SpeechGenerateOptions{Text: "boom", ProviderOptions: withTS})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range res.Warnings {
		found = found || w.Feature == "providerOptions.elevenlabs.withTimestamps"
	}
	if (*calls)[0].path != "/v1/sound-generation" || (*calls)[0].body["with_timestamps"] != nil || !found {
		t.Errorf("sound effects: %+v %v", (*calls)[0], res.Warnings)
	}
}
