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

// TS exposes the parsed JSON as response.body; raw bytes would marshal as base64.
func TestAzureSpeechTranscriptionResponseBodyIsParsedJSON(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
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
	res, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	body, ok := res.Response.Body.(map[string]interface{})
	if !ok {
		t.Fatalf("response body = %T, want parsed JSON object", res.Response.Body)
	}
	if _, ok := body["combinedPhrases"]; !ok {
		t.Fatalf("response body missing combinedPhrases: %v", body)
	}
}
