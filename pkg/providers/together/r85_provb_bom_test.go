package together

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// R85 reviewer item 3: a BOM-prefixed image response parses as in TS.
func TestR85provbImageBOMBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("\xEF\xBB\xBF{\"data\":[{\"b64_json\":\"aGk=\"}]}"))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("black-forest-labs/FLUX.1-schnell")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	if err != nil || res == nil || len(res.Images) != 1 || string(res.Images[0]) != "hi" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}
