package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR97provcImageKeyPassesThroughWithImages: falImageResponseSchema is
// z.union([base.extend({images}), base.extend({image})]) over a looseObject
// base. When `images` is present the first branch matches and `image` passes
// through unparsed, so providerMetadata.fal.image keeps its undeclared keys
// (zod 4.6.7: {"images":[{"url":"u"}],"image":{"url":"x","extra":1}}). Only
// an image-only body parses `image` with falImageSchema (stripped).
func TestR97provcImageKeyPassesThroughWithImages(t *testing.T) {
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G'})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/only") {
			_, _ = w.Write([]byte(`{"image":{"url":"` + srvURL + `/i.png","extra":1}}`))
			return
		}
		_, _ = w.Write([]byte(`{"images":[{"url":"` + srvURL + `/a.png"}],"image":{"url":"x","extra":1}}`))
	}))
	defer srv.Close()
	srvURL = srv.URL
	p := New(Config{APIKey: "k", BaseURL: srv.URL})

	cases := []struct {
		model string
		want  map[string]interface{}
	}{
		{"fal-ai/both", map[string]interface{}{"url": "x", "extra": float64(1)}},
		{"fal-ai/only", map[string]interface{}{"url": srvURL + "/i.png"}},
	}
	for _, c := range cases {
		m, err := p.ImageModel(c.model)
		if err != nil {
			t.Fatal(err)
		}
		res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
		if err != nil {
			t.Fatalf("%s: %v", c.model, err)
		}
		fal, _ := res.ProviderMetadata["fal"].(map[string]interface{})
		if got := fal["image"]; !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: fal.image = %#v, want %#v", c.model, got, c.want)
		}
	}
}
