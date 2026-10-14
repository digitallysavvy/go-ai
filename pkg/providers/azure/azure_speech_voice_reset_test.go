package azure

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ported from TS #21944: the Envoy "protocol error" 502 for unknown voices is
// a non-retryable 400; other 502s stay retryable.
func TestAzureSpeech_VoiceReset502IsNonRetryable400(t *testing.T) {
	speechErr := func(t *testing.T, status int, body string) *providererrors.ProviderError {
		t.Helper()
		server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		defer server.Close()
		p, err := New(Config{APIKey: "k", ResourceName: "r", SpeechBaseURL: server.URL})
		if err != nil {
			t.Fatal(err)
		}
		model, err := p.SpeechModel("mai-voice-2.1")
		if err != nil {
			t.Fatal(err)
		}
		_, err = model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hi", Voice: "x"})
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) {
			t.Fatalf("err = %v, want ProviderError", err)
		}
		return perr
	}

	perr := speechErr(t, 502, "upstream connect error or disconnect/reset before headers. reset reason: protocol error")
	if perr.StatusCode != 400 || perr.IsRetryable() {
		t.Errorf("status=%d retryable=%v, want 400 non-retryable", perr.StatusCode, perr.IsRetryable())
	}
	want := "Azure Speech could not synthesize the request. Check that the voice is available for this model and that the style is supported by the voice."
	if perr.Message != want {
		t.Errorf("message = %q", perr.Message)
	}

	perr = speechErr(t, 502, "bad gateway")
	if perr.StatusCode != 502 || !perr.IsRetryable() {
		t.Errorf("other 502: status=%d retryable=%v, want 502 retryable", perr.StatusCode, perr.IsRetryable())
	}
}
