package azure

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r88provbRequest is one request a test server received.
type r88provbRequest struct {
	path  string
	json  map[string]interface{}
	forms map[string]string
}

// r88provbServer answers every request with body and records it.
func r88provbServer(t *testing.T, contentType, body string) (*httptest.Server, *[]r88provbRequest) {
	t.Helper()
	var reqs []r88provbRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := r88provbRequest{path: r.URL.Path, forms: map[string]string{}}
		raw, _ := io.ReadAll(r.Body)
		if mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); strings.HasPrefix(mt, "multipart/") {
			mr := multipart.NewReader(strings.NewReader(string(raw)), params["boundary"])
			for {
				part, err := mr.NextPart()
				if err != nil {
					break
				}
				v, _ := io.ReadAll(part)
				rec.forms[part.FormName()] = string(v)
			}
		} else {
			_ = json.Unmarshal(raw, &rec.json)
		}
		reqs = append(reqs, rec)
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

// TS 6400cf8b88 (azure-openai-provider.test.ts): Azure chat merges openai
// and azure options, azure winning.
func TestR88provbAzureChatOptionNamespaces(t *testing.T) {
	srv, reqs := r88provbServer(t, "application/json", `{"id":"c","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.ChatModel("test-deployment")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		opts map[string]interface{}
		want map[string]interface{}
	}{
		{map[string]interface{}{"azure": map[string]interface{}{"user": "azure-user"}}, map[string]interface{}{"user": "azure-user"}},
		{map[string]interface{}{
			"openai": map[string]interface{}{"user": "openai-user", "logitBias": map[string]interface{}{"50256": -100}},
			"azure":  map[string]interface{}{"user": "azure-user"},
		}, map[string]interface{}{"user": "azure-user", "logit_bias": map[string]interface{}{"50256": float64(-100)}}},
	} {
		*reqs = nil
		if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}, ProviderOptions: tc.opts}); err != nil {
			t.Fatal(err)
		}
		for k, v := range tc.want {
			if !reflect.DeepEqual((*reqs)[0].json[k], v) {
				t.Errorf("%v: %s = %v, want %v", tc.opts, k, (*reqs)[0].json[k], v)
			}
		}
	}
	// An invalid azure option fails like an invalid openai one.
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}, ProviderOptions: map[string]interface{}{"azure": map[string]interface{}{"user": 5}}})
	if err == nil || !strings.Contains(err.Error(), "invalid azure provider options") {
		t.Fatalf("err = %v, want invalid azure provider options", err)
	}
}

// TS 6400cf8b88 (azure-transcription-model.test.ts): OpenAI transcription
// options under azure reach the OpenAI API (azure winning, no MAI language
// warning) and warn on the Azure Speech API.
func TestR88provbAzureTranscriptionOpenAIOptions(t *testing.T) {
	srv, reqs := r88provbServer(t, "application/json", `{"text":"hello","language":"en","duration":1,"combinedPhrases":[{"text":"hello"}],"phrases":[]}`)
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL, ResourceName: "r", SpeechBaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	whisper, _ := p.TranscriptionModel("whisper-1")
	audio := &provider.TranscriptionOptions{Audio: []byte{0xff, 0xf3, 0x44, 0xc4}, MimeType: "audio/mpeg"}

	audio.ProviderOptions = map[string]interface{}{"azure": map[string]interface{}{"language": "en", "prompt": "Names"}}
	res, err := whisper.DoTranscribe(context.Background(), audio)
	if err != nil {
		t.Fatal(err)
	}
	if f := (*reqs)[0].forms; f["language"] != "en" || f["prompt"] != "Names" {
		t.Errorf("form = %v, want language en, prompt Names", f)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", res.Warnings)
	}

	*reqs = nil
	audio.ProviderOptions = map[string]interface{}{
		"openai": map[string]interface{}{"language": "de", "prompt": "Names"},
		"azure":  map[string]interface{}{"language": "en"},
	}
	if _, err := whisper.DoTranscribe(context.Background(), audio); err != nil {
		t.Fatal(err)
	}
	if f := (*reqs)[0].forms; f["language"] != "en" || f["prompt"] != "Names" {
		t.Errorf("form = %v, want language en, prompt Names", f)
	}

	*reqs = nil
	mai, _ := p.TranscriptionModel("mai-transcribe-2")
	audio.ProviderOptions = map[string]interface{}{"azure": map[string]interface{}{"prompt": "Names"}}
	res, err = mai.DoTranscribe(context.Background(), audio)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains((*reqs)[0].path, "/speechtotext/") {
		t.Errorf("path = %s, want the Speech API", (*reqs)[0].path)
	}
	want := []types.Warning{{Type: "unsupported", Feature: "providerOptions.azure.prompt", Details: "This option requires the OpenAI transcription API."}}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Errorf("warnings = %+v, want %+v", res.Warnings, want)
	}

	// The OpenAI options are validated under azure too.
	audio.ProviderOptions = map[string]interface{}{"azure": map[string]interface{}{"temperature": 5}}
	if _, err := whisper.DoTranscribe(context.Background(), audio); err == nil {
		t.Fatal("want a validation error for azure.temperature 5")
	}
}

// TS 6400cf8b88 (azure-speech-model.test.ts).
func TestR88provbAzureSpeechOpenAIOptions(t *testing.T) {
	srv, reqs := r88provbServer(t, "audio/mpeg", "ID3")
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL, ResourceName: "r", SpeechBaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	tts, _ := p.SpeechModel("tts-1")
	res, err := tts.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello", ProviderOptions: map[string]interface{}{
		"openai": map[string]interface{}{"speed": 2},
		"azure":  map[string]interface{}{"instructions": "Speak calmly", "speed": 1.5},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if b := (*reqs)[0].json; b["instructions"] != "Speak calmly" || b["speed"] != 1.5 {
		t.Errorf("body = %v", b)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", res.Warnings)
	}

	voice, _ := p.SpeechModel("mai-voice-2")
	res, err = voice.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello", Voice: "en-US-Jasper:MAI-Voice-2", ProviderOptions: map[string]interface{}{"azure": map[string]interface{}{"speed": 1.5}}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range res.Warnings {
		found = found || w == (types.Warning{Type: "unsupported", Feature: "providerOptions.azure.speed", Details: "This option requires the OpenAI speech API."})
	}
	if !found {
		t.Errorf("warnings = %+v, want the OpenAI speech API warning", res.Warnings)
	}
}

// TS 6400cf8b88 (azure-mai-image-model.test.ts "accepts OpenAI options under
// the azure key on the OpenAI API").
func TestR88provbAzureImageOpenAIOptions(t *testing.T) {
	srv, reqs := r88provbServer(t, "application/json", `{"data":[{"b64_json":"aGk="}]}`)
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := p.ImageModel("gpt-image-1")
	if _, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x", ProviderOptions: map[string]interface{}{
		"openai": map[string]interface{}{"quality": "low", "background": "opaque"},
		"azure":  map[string]interface{}{"quality": "high"},
	}}); err != nil {
		t.Fatal(err)
	}
	if b := (*reqs)[0].json; b["quality"] != "high" || b["background"] != "opaque" {
		t.Errorf("body = %v", b)
	}
}
