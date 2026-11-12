package azure

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"golang.org/x/net/websocket"
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

// TestR94provbMaiTranscriptionAbortCause (cr-r94-prova-to-provb): TS
// connectToWebSocket fails the stream with abortSignal.reason.
func TestR94provbMaiTranscriptionAbortCause(t *testing.T) {
	ts := httptest.NewServer(websocket.Server{Handler: func(conn *websocket.Conn) {
		_, _ = io.Copy(io.Discard, conn)
	}})
	defer ts.Close()
	m := newAzureMaiTranscriptionModel("mai-transcribe", func() (string, error) { return ts.URL, nil },
		func(context.Context) (map[string]string, error) { return map[string]string{}, nil })
	cause := errors.New("r94provb caller cancelled")
	ctx, cancel := context.WithCancelCause(context.Background())
	audio := &r94provbBlockingAudio{}
	res, err := m.DoStream(ctx, &provider.TranscriptionStreamOptions{Audio: audio, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"}}, AzureTranscriptionModelOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Stream.Close() //nolint:errcheck
	if _, err := res.Stream.Next(); err != nil { // stream-start
		t.Fatal(err)
	}
	cancel(cause)
	for {
		if _, err = res.Stream.Next(); err != nil {
			break
		}
	}
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want the caller's cause", err)
	}
}

type r94provbBlockingAudio struct{}

func (r94provbBlockingAudio) Next(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (r94provbBlockingAudio) Cancel(error) {}

// TestR94provbMaiWebSocketURLIsWHATWG: TS toWebSocketUrl is new URL.
func TestR94provbMaiWebSocketURLIsWHATWG(t *testing.T) {
	if got, err := toWebSocketURL("https://R.services.ai.azure.com:443/mai/v1/realtime?intent=transcription"); err != nil || got != "wss://r.services.ai.azure.com/mai/v1/realtime?intent=transcription" {
		t.Fatalf("url = %q, %v", got, err)
	}
}
