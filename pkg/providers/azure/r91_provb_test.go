package azure

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"golang.org/x/net/websocket"
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
	_, err = mai.call(context.Background(), nil, nil, providerutils.OrderedMap{}, nil, nil)
	check("mai image", err)
}

// PB-14 (provb1-07): a close before the transcript completed reports TS's
// " (code N: reason)" suffix (azure-mai-transcription-model.ts:349-355).
func TestR91provbMaiCloseCodeSuffix(t *testing.T) {
	ts := httptest.NewServer(websocket.Server{Handler: func(conn *websocket.Conn) {
		conn.PayloadType = websocket.CloseFrame
		_, _ = conn.Write([]byte{0x0f, 0xa0, 'b', 'y', 'e'}) // 4000 "bye"
		time.Sleep(50 * time.Millisecond)
	}})
	defer ts.Close()
	p, err := New(Config{APIKey: "k", ResourceName: "r", MaiBaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := p.TranscriptionModel("mai-transcribe-2-streaming")
	rate := 16000
	result, err := model.(provider.TranscriptionStreamer).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio: newChanAudioStream([]byte{1, 2}), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	for {
		_, nextErr := result.Stream.Next()
		if nextErr == nil {
			continue
		}
		if nextErr == io.EOF || !strings.HasSuffix(nextErr.Error(), "closed before the transcript completed (code 4000: bye)") {
			t.Fatalf("err = %v", nextErr)
		}
		return
	}
}
