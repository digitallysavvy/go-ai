package gladia

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS getArgs rebuilds vocabulary objects as {value, intensity,
// pronunciations, language} (null/unknown keys dropped) and sends
// code_switching_config without `languages` when it is unset.
func TestGladiaRequestBodyMatchesTSMapping(t *testing.T) {
	var body map[string]interface{}
	var pollUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/upload":
			_, _ = w.Write([]byte(`{"audio_url":"u"}`))
		case "/v2/pre-recorded":
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = w.Write([]byte(`{"result_url":"http://` + r.Host + `/v2/job"}`))
		case "/v2/job":
			pollUA = r.Header.Get("User-Agent")
			_, _ = w.Write([]byte(`{"status":"done","result":{"metadata":{"audio_duration":1},"transcription":{"full_transcript":"x","languages":["en"],"utterances":[]}}}`))
		}
	}))
	defer server.Close()

	model, _ := New(Config{APIKey: "k", BaseURL: server.URL + "/v2"}).TranscriptionModel("default")
	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("a"), MimeType: "audio/mpeg",
		ProviderOptions: map[string]interface{}{"gladia": map[string]interface{}{
			"codeSwitchingConfig": map[string]interface{}{},
			"customVocabularyConfig": map[string]interface{}{"vocabulary": []interface{}{
				"plain",
				map[string]interface{}{"value": "v", "intensity": nil, "language": "en", "extra": 1},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cs, _ := body["code_switching_config"].(map[string]interface{}); cs == nil || len(cs) != 0 {
		t.Errorf("code_switching_config = %#v, want {}", body["code_switching_config"])
	}
	vocab := body["custom_vocabulary_config"].(map[string]interface{})["vocabulary"].([]interface{})
	entry := vocab[1].(map[string]interface{})
	if len(entry) != 2 || entry["value"] != "v" || entry["language"] != "en" {
		t.Errorf("vocabulary entry = %#v", entry)
	}
	if !strings.Contains(pollUA, "ai-sdk-gladia/") {
		t.Errorf("poll User-Agent = %q, want provider suffix", pollUA)
	}
}

// A provider called directly with a base64 audio string (TS string audio)
// uploads the decoded bytes, accepting unpadded/base64url input.
func TestGladiaUploadsDecodedAudioBase64(t *testing.T) {
	var uploaded []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/upload":
			f, _, err := r.FormFile("audio")
			if err != nil {
				t.Error(err)
				return
			}
			uploaded, _ = io.ReadAll(f)
			_, _ = w.Write([]byte(`{"audio_url":"u"}`))
		case "/v2/pre-recorded":
			_, _ = w.Write([]byte(`{"result_url":"http://` + r.Host + `/v2/job"}`))
		default:
			_, _ = w.Write([]byte(`{"status":"done","result":{"metadata":{"audio_duration":1},"transcription":{"full_transcript":"x","languages":[],"utterances":[]}}}`))
		}
	}))
	defer server.Close()
	model, _ := New(Config{APIKey: "k", BaseURL: server.URL + "/v2"}).TranscriptionModel("default")
	raw := []byte{0xfb, 0xff, 0xfe, 0x01}
	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		AudioBase64: base64.RawURLEncoding.EncodeToString(raw), MimeType: "audio/mpeg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(uploaded) != string(raw) {
		t.Fatalf("uploaded %v, want %v", uploaded, raw)
	}
}
