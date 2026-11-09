package quiverai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func r90toolsCapture(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","created":1,"data":[{"svg":"<svg/>","mime_type":"image/svg+xml"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &body
}

// TS quiverai-image-model.ts 300-355 postJsonToApi JSON.stringify of the
// body literal {model, n, prompt, ...sharedOptions, instructions,
// references}: Go marshalled a map with sorted keys.
func TestR90toolsQuiverAIGenerateBodyKeyOrder(t *testing.T) {
	srv, body := r90toolsCapture(t)
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ModelArrow2)
	n := 1
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "logo", N: &n,
		ProviderOptions: map[string]interface{}{"quiverai": map[string]interface{}{
			"temperature": 0.5, "instructions": "flat",
			"attributes": map[string]interface{}{"viewBox": map[string]interface{}{"height": 2, "width": 3, "minY": 0, "minX": 1}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"model":"arrow-2","n":1,"prompt":"logo","temperature":0.5,"attributes":{"viewBox":{"minX":1,"minY":0,"width":3,"height":2}},"stream":false,"instructions":"flat"}`
	if *body != want {
		t.Errorf("body\n got %s\nwant %s", *body, want)
	}
}

// TS rejectEditOnlyOptions / buildEditRequestBody (quiverai-image-model.ts
// 519-569, 571-589) test `value == null`: an empty referenceImages list is
// given, so generate rejects it as edit-only and edit sends
// `reference_images: []`.
func TestR90toolsQuiverAIEmptyReferenceImages(t *testing.T) {
	srv, body := r90toolsCapture(t)
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ModelArrow2)
	empty := map[string]interface{}{"quiverai": map[string]interface{}{"referenceImages": []interface{}{}}}
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{Prompt: "logo", ProviderOptions: empty})
	if err == nil || !strings.Contains(err.Error(), "edit-only provider options: referenceImages") {
		t.Fatalf("generate error = %v", err)
	}
	edit := map[string]interface{}{"quiverai": map[string]interface{}{"operation": "edit", "referenceImages": []interface{}{}}}
	_, err = model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "make it red", ProviderOptions: edit,
		Files: []provider.ImageFile{{Type: "file", Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), MediaType: "image/svg+xml"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(*body, `"reference_images":[]`) || !strings.HasPrefix(*body, `{"model":"arrow-2","prompt":"make it red","svg_source":`) {
		t.Errorf("edit body = %s", *body)
	}
}
