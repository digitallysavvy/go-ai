package googlevertex

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// End to end: core serializes the legacy URL string (gs://b/My%20File.pdf)
// and keeps the caller's string as OriginalURL; Vertex sends that original
// for a gs: URL (TS `originalUrl ?? url.toString()`), and the serialized
// form for https.
func TestR84integVertexGCSURLSentAsWritten(t *testing.T) {
	var mu sync.Mutex
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`))
	}))
	defer server.Close()
	prov, err := New(Config{Project: "p", Location: "us-central1", AccessToken: "t", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, err := prov.LanguageModel("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model: model,
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{MediaType: "application/pdf", URL: "gs://b/My File.pdf"},
			types.FileContent{MediaType: "application/pdf", URL: "HTTPS://X.test/a b.pdf"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, want := range []string{`"fileUri":"gs://b/My File.pdf"`, `"fileUri":"https://x.test/a%20b.pdf"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %s, want %s", body, want)
		}
	}
}
