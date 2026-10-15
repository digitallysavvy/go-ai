package azure

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS builds the definition with JSON.stringify, which drops undefined members:
// unset options must be omitted rather than sent as null.
func TestAzureSpeechTranscriptionDefinitionOmitsUnsetOptions(t *testing.T) {
	var definition string
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		definition = r.FormValue("definition")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"combinedPhrases": [{"text": "hi"}], "phrases": []}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "k", ResourceName: "r", SpeechBaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, err := p.TranscriptionModel("mai-transcribe-1.5")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(definition, "null") {
		t.Fatalf("definition contains null members: %s", definition)
	}
}
