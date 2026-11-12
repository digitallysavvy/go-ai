package azure

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR94provbSpeechVoiceEmptyLanguageCode: TS resolveVoice computes
// `language ? language.split('-')[0].toLowerCase() : undefined`, so a
// language such as "-US" is the code "" (not undefined): with no voice it has
// no default voice and warns, and with an explicit voice it differs from the
// voice's language and warns.
func TestR94provbSpeechVoiceEmptyLanguageCode(t *testing.T) {
	voice, warning := azureResolveVoice("", "-US")
	if voice != azureDefaultVoice || warning != `No default MAI voice for language "-US". en-US-Harper was used.` {
		t.Fatalf("no voice: got %q, %q", voice, warning)
	}
	voice, warning = azureResolveVoice("de-DE-Mia", "-US")
	if voice != "de-DE-Mia" || warning != `The voice de-DE-Mia selects the language. Language "-US" was ignored.` {
		t.Fatalf("explicit voice: got %q, %q", voice, warning)
	}
	if voice, warning = azureResolveVoice("", ""); voice != azureDefaultVoice || warning != "" {
		t.Fatalf("no language: got %q, %q", voice, warning)
	}
}

// TestR94provbMaiImageHeadersBeforeDownloadOnce (cr-r94-tools-to-provb): TS
// resolves requestOptions.headers once, before it downloads the input files,
// and reuses them for each of the n requests.
func TestR94provbMaiImageHeadersBeforeDownloadOnce(t *testing.T) {
	keyErr := errors.New("r94provb token")
	mai := &azureMaiImageModel{modelID: "mai-image-2.5", url: func(string) (string, error) { return "https://example.invalid", nil },
		headers: func(context.Context) (map[string]string, error) { return nil, keyErr }, httpClient: stdhttp.DefaultClient}
	_, err := mai.doGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x", Files: []provider.ImageFile{{Type: "url", URL: "https://127.0.0.1:1/x.png"}}}, AzureImageModelOptions{})
	if !errors.Is(err, keyErr) {
		t.Fatalf("err = %T %v, want the header error", err, err)
	}

	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"AA=="}]}`))
	}))
	defer srv.Close()
	var calls atomic.Int32
	mai = &azureMaiImageModel{modelID: "mai-image-2.5", url: func(path string) (string, error) { return srv.URL + path, nil },
		headers: func(context.Context) (map[string]string, error) { calls.Add(1); return map[string]string{"api-key": "k"}, nil }, httpClient: srv.Client()}
	n := 2
	res, err := mai.doGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x", N: &n}, AzureImageModelOptions{})
	if err != nil || len(res.Images) != 2 || calls.Load() != 1 {
		t.Fatalf("err = %v, images %d, header calls %d; want 2 images and 1 call", err, len(res.Images), calls.Load())
	}
}
