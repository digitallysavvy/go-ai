package fal

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// z.number() rejects Infinity: a code past the double range does not match
// falErrorDataSchema, so there is no data (and the status text message).
func TestR99provcFalErrorDataRejectsInfiniteCode(t *testing.T) {
	statusErr := &internalhttp.HTTPStatusError{StatusCode: 500, StatusText: "Phrase", Body: []byte(`{"error":{"message":"m","code":1e999}}`)}
	err := falAPIError("fal", statusErr, statusErr)
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Phrase" || pe.Data != nil {
		t.Fatalf("err = %#v", err)
	}
}

// getFromApi's 'Failed to process error response' APICallError has the
// status and headers but no responseBody.
func TestR99provcFalTranscriptionFailedToProcessHasNoBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/wizper", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"request_id":"id"}`)
	})
	mux.HandleFunc("/fal-ai/wizper/requests/id", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "<html>502</html>")
	})
	m, srv := newTestTranscriptionModel(t, mux)
	defer srv.Close()
	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Failed to process error response" || pe.ResponseBody != "" || pe.Data != nil {
		t.Fatalf("err = %#v", err)
	}
}

// The generated-image download is a getFromApi with no headers: the runtime
// tag only; a 204 has a null body (createBinaryResponseHandler).
func TestR99provcFalImageDownloadUserAgentAndNoContent(t *testing.T) {
	var ua string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		w.WriteHeader(status)
	}))
	defer srv.Close()
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "fal-ai/flux/dev")
	if _, err := m.downloadImage(context.Background(), srv.URL+"/img.png"); err != nil {
		t.Fatal(err)
	}
	if ua != providerutils.RuntimeEnvironmentUserAgent() {
		t.Errorf("User-Agent = %q", ua)
	}
	status = http.StatusNoContent
	_, err := m.downloadImage(context.Background(), srv.URL+"/img.png")
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Response body is empty" || pe.StatusCode != 204 {
		t.Fatalf("204: err = %#v", err)
	}
}

// The speech audio download is a binary getFromApi: a 205 has a null body.
func TestR99provcFalSpeechAudioResetContent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/minimax/speech-02-hd", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"audio":{"url":"http://` + r.Host + `/files/test.mp3"}}`))
	})
	mux.HandleFunc("/files/test.mp3", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusResetContent) })
	m, srv := newTestSpeechModel(t, mux)
	defer srv.Close()
	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Response body is empty" || pe.StatusCode != 205 {
		t.Fatalf("err = %#v", err)
	}
}

// audio.url is z.string(): "" is fetched and fails getFromApi's URL check
// (a DownloadError), not a Go-only invalid-response error.
func TestR99provcFalSpeechEmptyAudioURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/minimax/speech-02-hd", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"audio":{"url":""}}`))
	})
	m, srv := newTestSpeechModel(t, mux)
	defer srv.Close()
	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	var de *providererrors.DownloadError
	if !errors.As(err, &de) {
		t.Fatalf("err = %T %v", err, err)
	}
}
