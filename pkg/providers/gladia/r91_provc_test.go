package gladia

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r91provcRun runs one transcription against a server whose poll answers
// pollBody; it returns the pre-recorded body, the result and the error.
func r91provcRun(t *testing.T, pollBody string, opts map[string]interface{}) (string, *types.TranscriptionResult, error) {
	t.Helper()
	var mu sync.Mutex
	var submitted string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/upload":
			_, _ = io.WriteString(w, `{"audio_url":"u"}`)
		case "/v2/pre-recorded":
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			submitted = string(b)
			mu.Unlock()
			_, _ = io.WriteString(w, `{"result_url":"http://`+r.Host+`/v2/job"}`)
		case "/v2/job":
			w.Header().Add("X-Test", "a")
			w.Header().Add("X-Test", "b")
			_, _ = io.WriteString(w, pollBody)
		}
	}))
	defer server.Close()
	model, _ := New(Config{APIKey: "k", BaseURL: server.URL + "/v2"}).TranscriptionModel("default")
	res, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("a"), MimeType: "audio/mpeg", ProviderOptions: opts,
	})
	mu.Lock()
	defer mu.Unlock()
	return submitted, res, err
}

// TestR91provcJobErrorsCarryTSNames (provc-17): TranscriptionJobFailed and
// TranscriptionResultEmpty carry their TS AISDKError names, and Data is the
// schema result (undeclared top-level keys stripped).
func TestR91provcJobErrorsCarryTSNames(t *testing.T) {
	for _, c := range []struct{ body, code string }{
		{`{"status":"error","error_code":500,"extra":1}`, "TranscriptionJobFailed"},
		{`{"status":"done","result":null,"extra":1}`, "TranscriptionResultEmpty"},
	} {
		_, _, err := r91provcRun(t, c.body, nil)
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) {
			t.Fatalf("%s: err = %T %v", c.code, err, err)
		}
		if perr.ErrorCode != c.code {
			t.Errorf("ErrorCode = %q, want %q", perr.ErrorCode, c.code)
		}
		if data, _ := perr.Data.(map[string]interface{}); data == nil || data["extra"] != nil {
			t.Errorf("%s: Data = %#v, want the schema result", c.code, perr.Data)
		}
	}
}

// TestR91provcBodyOrderAndHeaders (provc-07, provc-06): the pre-recorded
// body keeps the TS getArgs order (nested configs included), and repeated
// response headers are joined with ", ".
func TestR91provcBodyOrderAndHeaders(t *testing.T) {
	submitted, res, err := r91provcRun(t, `{"status":"done","result":{"metadata":{"audio_duration":1},"transcription":{"full_transcript":"x","languages":["en"],"utterances":[]}}}`,
		map[string]interface{}{"gladia": map[string]interface{}{
			"diarization": true, "sentences": true, "callback": false,
			"diarizationConfig":      map[string]interface{}{"maxSpeakers": 3, "minSpeakers": 1},
			"languageConfig":         map[string]interface{}{"codeSwitching": true, "languages": []interface{}{"en"}},
			"translationConfig":      map[string]interface{}{"targetLanguages": []interface{}{"fr"}, "model": "base"},
			"customVocabularyConfig": map[string]interface{}{"defaultIntensity": 0.5, "vocabulary": []interface{}{map[string]interface{}{"language": "en", "value": "v"}}},
		}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"callback":false,"diarization":true,"sentences":true,"custom_vocabulary_config":{"vocabulary":[{"value":"v","language":"en"}],"default_intensity":0.5},"language_config":{"languages":["en"],"code_switching":true},"diarization_config":{"min_speakers":1,"max_speakers":3},"translation_config":{"target_languages":["fr"],"model":"base"},"audio_url":"u"}`; submitted != want {
		t.Errorf("body = %s\nwant %s", submitted, want)
	}
	if got := res.Response.Headers["X-Test"]; got != "a, b" {
		t.Errorf("X-Test = %q", got)
	}
}
