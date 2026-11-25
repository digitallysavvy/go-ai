package replicate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// The prediction ID comes from the create response: the poll URL carries it
// as one encoded path segment (like the image model's cancel URL and TS
// encodePathSegment), so a crafted ID cannot reach another endpoint.
func TestR95provcLanguageModelPollEncodesPredictionID(t *testing.T) {
	const rawID = "p1/../../account"
	var pollURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"` + rawID + `","status":"starting"}`))
			return
		}
		pollURI = r.RequestURI
		_, _ = w.Write([]byte(`{"id":"p1","status":"succeeded","output":"hi"}`))
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "v1")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil || res.Text != "hi" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if want := "/predictions/" + providerutils.EncodePathSegment(rawID); pollURI != want {
		t.Errorf("poll URI = %q, want %q", pollURI, want)
	}
}
