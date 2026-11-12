package googlevertex

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Cloud TTS is TS postJsonToApi (`!response.ok` runs the failed-response
// handler): an unfollowed 3xx is the Vertex API error, not a success.
func TestR94provgCloudTTSNon2xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(300)
		_, _ = w.Write([]byte(`{"error":{"code":300,"message":"moved","status":"X"},"audioContent":"AAAA"}`))
	}))
	defer srv.Close()
	model, err := newChirpProvider(t, srv.URL).SpeechModel("chirp-3-hd")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 300 || pe.Message != "moved" {
		t.Fatalf("err = %T %v, want the 300 API error", err, err)
	}
}
