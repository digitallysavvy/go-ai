package elevenlabs

import (
	"context"
	"errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS lets a transport failure propagate unchanged and keeps the failed
// response body, data and lower-case headers on an HTTP error (APICallError).
func TestHandleErrorKeepsTransportFailuresAndResponseData(t *testing.T) {
	sentinel := errors.New("Cannot connect to API: boom")
	if got := handleTranscriptionError(sentinel); got != sentinel {
		t.Fatalf("transport failure was wrapped: %v", got)
	}

	body := `{"error":{"message":"m","code":400}}`
	got := handleTranscriptionError(&internalhttp.HTTPStatusError{StatusCode: 400, Headers: http.Header{"X-Request-Id": {"r1"}}, Body: []byte(body)})
	var perr *providererrors.ProviderError
	if !errors.As(got, &perr) || perr.StatusCode != 400 || perr.ResponseBody != body || perr.Data == nil || perr.ResponseHeaders["X-Request-Id"] != "r1" {
		t.Fatalf("got %#v", perr)
	}
}

func TestSpeechTransportFailureIsNotWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).SpeechModel("eleven_multilingual_v2")
	server.Close()
	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "p"})
	var perr *providererrors.ProviderError
	if err == nil || errors.As(err, &perr) || !strings.HasPrefix(err.Error(), "Cannot connect to API") {
		t.Fatalf("err = %#v", err)
	}
}
