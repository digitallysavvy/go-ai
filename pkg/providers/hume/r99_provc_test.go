package hume

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createBinaryResponseHandler: a 204 audio reply has a null body, the
// APICallError "Response body is empty" (cr-r99-tools-to-provc).
func TestR99provcSpeechNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel("")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Response body is empty" || pe.StatusCode != 204 || pe.Provider != "hume" {
		t.Fatalf("err = %#v", err)
	}
}
