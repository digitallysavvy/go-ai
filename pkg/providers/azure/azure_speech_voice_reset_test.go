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

// TS c3b6fef1ff ("keeps 502 responses retryable"): every 502, the Envoy
// "protocol error" reset included, keeps its status, the standard message
// and its retryability.
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

	for _, body := range []string{
		"upstream connect error or disconnect/reset before headers. reset reason: protocol error",
		"upstream connect error or disconnect/reset before headers. reset reason: connection termination",
		"unavailable",
	} {
		perr := speechErr(t, 502, body)
		if perr.StatusCode != 502 || !perr.IsRetryable() || perr.Message != "Azure Speech request failed with status 502." || perr.ResponseBody != body {
			t.Errorf("%q: status=%d retryable=%v message=%q", body, perr.StatusCode, perr.IsRetryable(), perr.Message)
		}
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
