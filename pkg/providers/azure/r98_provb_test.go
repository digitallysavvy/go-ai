package azure

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// TestR98provbMaiJSNumberPlatformInt: a metadata token count is the JS
// number; an integral count outside the platform int (32-bit targets) stays
// a float64 instead of overflowing the int conversion.
func TestR98provbMaiJSNumberPlatformInt(t *testing.T) {
	for in, want := range map[float64]string{3e9: "3000000000", 12: "12", 10.5: "10.5"} {
		b, _ := jsonstr.Marshal(azureMaiJSNumber(in))
		if string(b) != want {
			t.Errorf("%v: %s, want %s", in, b, want)
		}
	}
}

// TestR98provbRuntimeUserAgent: TS postToApi / postFormDataToApi /
// postJsonToApi append the runtime user-agent tag, so the Azure speech,
// speech transcription and MAI image requests carry it after the provider
// tag. Go sent these with a raw http client and only the provider tag.
func TestR98provbRuntimeUserAgent(t *testing.T) {
	var ua string
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		ua = r.Header.Get("User-Agent")
		switch {
		case strings.Contains(r.URL.Path, "/mai/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(maiImageResponse))
		case strings.Contains(r.URL.Path, "transcri"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"combinedPhrases":[{"text":"a"}],"phrases":[]}`))
		default:
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte{1})
		}
	}))
	defer server.Close()
	p, err := New(Config{APIKey: "k", ResourceName: "r", SpeechBaseURL: server.URL, MaiBaseURL: server.URL + "/mai/v1/", BaseURL: server.URL + "/openai"})
	if err != nil {
		t.Fatal(err)
	}
	want := " " + providerutils.RuntimeEnvironmentUserAgent()
	check := func(name string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(ua, "ai-sdk-azure/") || !strings.HasSuffix(ua, want) {
			t.Errorf("%s: User-Agent = %q, want the provider tag then the runtime tag", name, ua)
		}
		ua = ""
	}
	speech, _ := p.SpeechModel("mai-voice-2.1")
	_, err = speech.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi", Voice: "en-US-Jasper:MAI-Voice-1"})
	check("speech", err)
	tr, _ := p.TranscriptionModel("mai-transcribe-2")
	_, err = tr.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	check("transcription", err)
	img, _ := p.ImageModel("MAI-Image-2.6-Flash")
	_, err = img.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	check("mai image", err)
}
