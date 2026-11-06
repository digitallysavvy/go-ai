package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS doStatus reads a failed status body with response.json(); a non-JSON
// body throws a SyntaxError (not an APICallError), so doStatus rejects rather
// than reporting an error status.
func TestR36_VideoStatusNonJSONFailureBodyRejects(t *testing.T) {
	for name, body := range map[string]string{"html": "<html>502</html>", "empty": "", "null": "null"} {
		mux := http.NewServeMux()
		mux.HandleFunc("/fal-ai/m", videoQueueHandler(t, "id", "/fal-ai/m/requests/id", nil, nil))
		mux.HandleFunc("/fal-ai/m/requests/id", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(body))
		})
		model, _ := newTestVideoModel(t, "m", mux)
		start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation})
		if err == nil {
			t.Errorf("%s: DoStatus = %+v, want an error", name, status)
		}
	}
}

// TS getFromApi with createStatusCodeErrorResponseHandler: a failed media
// download is an APICallError (message = status text), not wrapped.
func TestFalFailedDownloadsAreStatusTextProviderErrors(t *testing.T) {
	check := func(t *testing.T, err error) {
		t.Helper()
		pe, ok := err.(*providererrors.ProviderError)
		if !ok || pe.StatusCode != 404 || pe.Message != "Not Found" {
			t.Fatalf("err = %v (%T)", err, err)
		}
	}
	t.Run("image", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/fal-fast" {
				_, _ = w.Write([]byte(`{"images":[{"url":"http://` + r.Host + `/img.png","content_type":"image/png"}]}`))
				return
			}
			http.Error(w, "gone", http.StatusNotFound)
		}))
		defer srv.Close()
		m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "fal-fast")
		_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
		check(t, err)
	})
	t.Run("speech", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/fal-ai/minimax/speech-02-hd", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"audio":{"url":"http://` + r.Host + `/files/missing.mp3"}}`))
		})
		mux.HandleFunc("/files/", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "gone", http.StatusNotFound) })
		m, srv := newTestSpeechModel(t, mux)
		defer srv.Close()
		_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
		check(t, err)
	})
}
