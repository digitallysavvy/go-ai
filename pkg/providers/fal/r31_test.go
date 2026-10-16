package fal

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestImageStatusErrorKeepsEmptyMessage(t *testing.T) {
	err := (&ImageModel{}).statusError(400, nil, []byte(`{"message":""}`))
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "" || perr.StatusCode != 400 {
		t.Fatalf("got %#v", perr)
	}
}

// TS records JSON.stringify(requestBody) in insertion order:
// text, output_format, voice, speed, then provider options.
func TestSpeechRequestBodyKeyOrder(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/minimax/speech-02-hd", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"audio":{"url":"http://` + r.Host + `/files/a.mp3"}}`))
	})
	mux.HandleFunc("/files/a.mp3", speechAudioHandler([]byte{1}, nil))
	m, srv := newTestSpeechModel(t, mux)
	defer srv.Close()
	speed := 1.5
	res, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "hi", Voice: "v", Speed: &speed,
		ProviderOptions: map[string]interface{}{"fal": map[string]interface{}{
			"zzz": 1, "language_boost": "Chinese",
			"voice_setting": map[string]interface{}{"pitch": 2.0, "speed": 1.0, "junk": true},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"text":"hi","output_format":"url","voice":"v","speed":1.5,"voice_setting":{"speed":1,"pitch":2},"language_boost":"Chinese","zzz":1}`
	if res.Request.Body != want {
		t.Fatalf("request.body = %s, want %s", res.Request.Body, want)
	}
}
