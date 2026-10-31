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

// TS createJsonErrorResponseHandler (transcription) uses the HTTP status text
// for an empty or unparseable error body and accepts both error shapes; the
// speech-synthesis handler only accepts {"error":{"message"}}.
func TestAzureSpeechErrors_MessageShapes(t *testing.T) {
	cases := []struct {
		name   string
		speech bool
		status int
		body   string
		want   string
	}{
		{"transcription empty body uses status text", false, 400, "", "Bad Request"},
		{"transcription unparseable uses status text", false, 503, "<html>", "Service Unavailable"},
		{"transcription error.message", false, 400, `{"error":{"message":"bad audio"}}`, "bad audio"},
		{"transcription top-level message", false, 400, `{"message":"bad audio"}`, "bad audio"},
		{"speech top-level message is not parsed", true, 500, `{"message":"x"}`, "Azure Speech request failed with status 500."},
		{"speech error.message", true, 400, `{"error":{"message":"bad voice"}}`, "bad voice"},
		{"speech empty 400", true, 400, "", "Azure Speech request failed with status 400. Check the voice name, style, and output format."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := azureSpeechAPIError("azure.test", tc.status, stdhttp.Header{}, []byte(tc.body), tc.speech)
			var perr *providererrors.ProviderError
			if !errors.As(err, &perr) {
				t.Fatalf("err = %v", err)
			}
			if perr.Message != tc.want {
				t.Errorf("message = %q, want %q", perr.Message, tc.want)
			}
		})
	}
}
