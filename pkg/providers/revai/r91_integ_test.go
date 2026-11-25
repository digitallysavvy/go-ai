package revai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// provc-07 (TS JSON.stringify(transcriptionModelOptions)): the config form
// field is transcriber, then the options in formDataConfig order.
func TestR91integConfigKeyOrder(t *testing.T) {
	var seen string
	p := newRevaiTestServer(t, func(path string, r *http.Request) {
		if path == "/speechtotext/v1/jobs" && r.ParseMultipartForm(10<<20) == nil {
			seen = r.FormValue("config")
		}
	})
	model, _ := p.TranscriptionModel(ModelMachine)
	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: revaiTestAudio, MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"revai": map[string]interface{}{"language": "en", "verbatim": true, "metadata": "m<1>"}}})
	if err != nil {
		t.Fatal(err)
	}
	// The schema defaults (rush, test_mode, ...) are filled in like TS zod.
	if want := `{"transcriber":"machine","metadata":"m<1>","verbatim":true,"rush":false,"test_mode":false,"skip_diarization":false,"skip_postprocessing":false,"skip_punctuation":false,"remove_disfluencies":false,"remove_atmospherics":false,"filter_profanity":false,"diarization_type":"standard","language":"en","forced_alignment":false}`; seen != want {
		t.Fatalf("config = %s, want %s", seen, want)
	}
}

// provc-17 (TS AISDKError names) and provc-06 (TS extractResponseHeaders).
func TestR91integJobErrorNamesAndHeaders(t *testing.T) {
	submit := map[string]interface{}{"id": "test-id", "created_on": "2026-02-12T23:26:22.276Z", "status": "failed", "type": "async"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(submit)
	}))
	defer srv.Close()
	model, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel(ModelMachine)
	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: revaiTestAudio, MimeType: "audio/wav"})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.ErrorCode != "TranscriptionJobSubmissionFailed" {
		t.Fatalf("submission err = %#v", err)
	}
	if got := flattenHeaders(map[string][]string{"x-test": {"a", "b"}, "x-latin": {"\xe9"}}); got["X-Test"] != "a, b" || got["X-Latin"] != "é" {
		t.Fatalf("headers = %q", got)
	}
}
