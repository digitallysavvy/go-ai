package googlevertex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"golang.org/x/net/websocket"
)

// TS google-vertex-transcription-model.ts posts with postJsonToApi, which
// tags the user-agent with the runtime after the provider tag.
func TestR98provgChirpRecognizeCarriesRuntimeUserAgent(t *testing.T) {
	var ua string
	p, err := New(Config{
		Project: "p", Location: "us-central1", AccessToken: "token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			ua = r.Header.Get("User-Agent")
			return jsonResponse(200, `{"results":[]}`), nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := p.TranscriptionModel("chirp_2")
	if _, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{AudioBase64: "AQID", MimeType: "audio/wav"}); err != nil {
		t.Fatal(err)
	}
	rt := providerutils.RuntimeEnvironmentUserAgent()
	if !strings.HasPrefix(ua, "ai-sdk-google-vertex/") || !strings.HasSuffix(ua, " "+rt) {
		t.Fatalf("user-agent = %q, want ai-sdk-google-vertex/... %s", ua, rt)
	}
}

// TS google-vertex-gemini-transcription-model.ts: JSON.stringify({ setup })
// with setup = {model, inputAudioTranscription} in
// buildAudioTranscriptionConfig order.
func TestR98provgVertexLiveTranscriptionSetupKeyOrder(t *testing.T) {
	first := make(chan string, 1)
	srv := httptest.NewServer(websocket.Server{Handler: func(conn *websocket.Conn) {
		var raw string
		if websocket.Message.Receive(conn, &raw) == nil {
			first <- raw
		}
		_ = conn.Close()
	}})
	defer srv.Close()
	model := newLiveTestModel(t, srv.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		ProviderOptions: map[string]interface{}{"googleVertex": map[string]interface{}{
			"mode": "SMART", "diarization": true, "wordTimestamp": true,
			"customVocabulary": []interface{}{"Gemini"}, "languageCodes": []interface{}{"en-US"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	var got string
	select {
	case got = <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("no setup frame")
	}
	want := regexp.MustCompile(`^\{"setup":\{"model":"projects/[^"]+","inputAudioTranscription":\{"languageCodes":\["en-US"\],"customVocabulary":\["Gemini"\],"wordTimestamp":true,"diarization":true,"mode":"SMART"\}\}\}$`)
	if !want.MatchString(got) {
		t.Fatalf("setup = %s", got)
	}
}
