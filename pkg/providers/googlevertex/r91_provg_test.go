package googlevertex

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// provg1-01 (typed-option probe): Vertex option readers accept Go-typed
// literals the way TS zod parses their JSON form.
func TestR91provgTypedOptionLiterals(t *testing.T) {
	emb := &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"googleVertex": map[string]string{"taskType": "RETRIEVAL_QUERY", "title": "t"}}}
	if err := validateVertexEmbeddingOptions(emb); err != nil {
		t.Fatalf("embedding validate: %v", err)
	}
	if got := vertexEmbeddingOptions(emb); got.TaskType != "RETRIEVAL_QUERY" || got.Title != "t" {
		t.Fatalf("embedding options = %+v", got)
	}
	gt, err := parseVertexGeminiTranscriptionOptionsChecked(map[string]interface{}{"vertex": map[string][]string{"languageCodes": {"en-US"}}})
	if err != nil || len(gt.LanguageCodes) != 1 {
		t.Fatalf("gemini transcription: %+v, %v", gt, err)
	}
	ct, err := vertexTranscriptionOptionsChecked(map[string]interface{}{"googleVertex": map[string]string{"region": "eu"}})
	if err != nil || ct.Region != "eu" {
		t.Fatalf("cloud transcription: %+v, %v", ct, err)
	}
	if img := extractVertexOptions(map[string]interface{}{"googleVertex": map[string]string{"negativePrompt": "x"}}); img["negativePrompt"] != "x" {
		t.Fatalf("image options = %#v", img)
	}
	vm := &VideoModel{}
	if poll := vm.getPollOptions(map[string]interface{}{"googleVertex": map[string]int64{"pollIntervalMs": 1234}}); poll.PollIntervalMs != 1234 {
		t.Fatalf("video poll = %+v", poll)
	}
}

func r91provgFailingProvider(t *testing.T) *Provider {
	t.Helper()
	failing := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: connection refused")
	})}
	p, err := New(Config{Project: "p", Location: "us-central1", AccessToken: "t", HTTPClient: failing, CloudTTSBaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func r91provgWantConnect(t *testing.T, what string, err error) {
	t.Helper()
	var r interface{ IsRetryable() bool }
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !errors.As(err, &r) || !r.IsRetryable() {
		t.Fatalf("%s: error = %T %v", what, err, err)
	}
}

// provg1-07 (Vertex transcription raw Do) and provg1-08 (Cloud TTS and
// embedding keep the client's connect error text, no prefix).
func TestR91provgTransportErrors(t *testing.T) {
	p := r91provgFailingProvider(t)
	tm, _ := p.TranscriptionModel("chirp_2")
	_, err := tm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{AudioBase64: "AQ=="})
	r91provgWantConnect(t, "transcription", err)
	sm, _ := p.SpeechModel("chirp-3-hd")
	_, err = sm.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	r91provgWantConnect(t, "cloud tts", err)
	em, _ := p.EmbeddingModel("text-embedding-005")
	_, err = em.DoEmbed(context.Background(), "hi", nil)
	r91provgWantConnect(t, "embedding", err)
}

// provc-06: video response headers join repeated values.
func TestR91provgVideoHeadersJoinValues(t *testing.T) {
	if got := convertVertexVideoHeaders(http.Header{"X-A": {"1", "2"}}); got["X-A"] != "1, 2" {
		t.Fatalf("headers = %v", got)
	}
}

// TS c160b74576: gemini-4-carbon model ID.
func TestR91provgGemini4CarbonID(t *testing.T) {
	if ModelGemini4Carbon != "gemini-4-carbon" {
		t.Fatal(ModelGemini4Carbon)
	}
}
