package azure

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// OVC-02 (prova-02 provb sites): a transport failure on the raw-client Azure
// Speech and MAI image calls is TS handleFetchError's retryable "Cannot
// connect to API" error, not a status-0 ProviderError.
func TestR91provbAzureRawClientTransportErrors(t *testing.T) {
	srv := httptest.NewServer(stdhttp.NotFoundHandler())
	url := srv.URL
	srv.Close() // connection refused from now on
	urlFn := func() (string, error) { return url, nil }
	headers := func(context.Context) (map[string]string, error) { return map[string]string{}, nil }
	check := func(name string, err error) {
		t.Helper()
		var r interface{ IsRetryable() bool }
		if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !errors.As(err, &r) || !r.IsRetryable() {
			t.Fatalf("%s: err = %v, want the retryable connect error", name, err)
		}
	}
	_, err := newAzureSpeechSpeechModel("m", urlFn, headers, stdhttp.DefaultClient).DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"}, AzureSpeechModelSpeechOptions{})
	check("speech", err)
	_, err = newAzureSpeechTranscriptionModel("m", urlFn, headers, stdhttp.DefaultClient).DoGenerate(context.Background(), &provider.TranscriptionOptions{Audio: []byte("RIFF"), MimeType: "audio/wav"}, AzureSpeechTranscriptionOptions{})
	check("transcription", err)
	mai := &azureMaiImageModel{modelID: "m", url: func(string) (string, error) { return url, nil }, headers: headers, httpClient: stdhttp.DefaultClient}
	_, err = mai.call(context.Background(), nil, providerutils.OrderedMap{}, nil, nil)
	check("mai image", err)
}
