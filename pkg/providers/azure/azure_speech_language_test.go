package azure

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS: `phrase.locale ? [...] : []` -- an empty locale contributes no language,
// so en + "" still reports "en".
func TestAzureSpeechTranscription_EmptyLocaleDoesNotHideLanguage(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"combinedPhrases":[{"text":"a b"}],"phrases":[{"text":"a","locale":"en-US"},{"text":"b","locale":""}]}`))
	}))
	defer server.Close()
	p, err := New(Config{APIKey: "k", ResourceName: "r", SpeechBaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := p.TranscriptionModel("mai-transcribe-2")
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Language != "en" {
		t.Fatalf("language = %q, want en", result.Language)
	}
}
