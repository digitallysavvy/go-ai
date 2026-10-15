package googlevertex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestVertexGroundingSources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"x"}],"role":"model"},"finishReason":"STOP","groundingMetadata":{"groundingChunks":[{"web":{"uri":"https://a.example","title":"A"}},{"retrievedContext":{"uri":"gs://b/c.txt","title":"C"}}]}}]}`))
	}))
	defer srv.Close()
	prov, err := New(Config{Project: "p", Location: "us-central1", AccessToken: "t", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, err := prov.LanguageModel("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 3 {
		t.Fatalf("content = %#v", res.Content)
	}
	a, _ := res.Content[1].(types.SourceContent)
	d, _ := res.Content[2].(types.SourceContent)
	if a.URL != "https://a.example" || a.Title != "A" || d.SourceType != "document" || d.MediaType != "text/plain" || d.Filename != "c.txt" {
		t.Fatalf("sources = %#v %#v", a, d)
	}
}
