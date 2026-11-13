package fal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS lets a transport failure propagate unchanged and keeps the failed
// response body, data and lower-case headers on an HTTP error (APICallError).
func TestHandleErrorsKeepTransportFailuresAndResponseData(t *testing.T) {
	sentinel := errors.New("Cannot connect to API: boom")
	body := `{"error":{"message":"m","code":400}}`
	httpErr := func() error {
		return &internalhttp.HTTPStatusError{StatusCode: 400, Headers: http.Header{"X-Request-Id": {"r1"}}, Body: []byte(body)}
	}
	for name, handle := range map[string]func(error) error{
		"speech":        (&SpeechModel{}).handleError,
		"transcription": (&TranscriptionModel{}).handleError,
		"video":         (&VideoModel{}).handleError,
	} {
		if got := handle(sentinel); got != sentinel {
			t.Errorf("%s: transport failure was wrapped: %v", name, got)
		}
		var perr *providererrors.ProviderError
		if !errors.As(handle(httpErr()), &perr) || perr.ResponseBody != body || perr.Data == nil || perr.ResponseHeaders["X-Request-Id"] != "r1" || perr.Message != "m" {
			t.Errorf("%s: got %#v", name, perr)
		}
	}
}

func TestImageTransportFailureIsNotWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "fal-ai/flux/dev")
	server.Close()
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	var perr *providererrors.ProviderError
	if err == nil || errors.As(err, &perr) || !strings.HasPrefix(err.Error(), "Cannot connect to API") {
		t.Fatalf("err = %#v", err)
	}
}
