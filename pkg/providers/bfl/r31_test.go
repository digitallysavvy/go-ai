package bfl

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS: url.searchParams.has('id') keeps an `id=` pair (even empty) and set()
// appends in place instead of sorting the query.
func TestEnsurePollingURLHasID(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.bfl.ai/v1/get_result?z=1":      "https://api.bfl.ai/v1/get_result?z=1&id=req+1",
		"https://api.bfl.ai/v1/get_result":          "https://api.bfl.ai/v1/get_result?id=req+1",
		"https://api.bfl.ai/v1/get_result?id=":      "https://api.bfl.ai/v1/get_result?id=",
		"https://api.bfl.ai/v1/get_result?id=x#f":   "https://api.bfl.ai/v1/get_result?id=x#f",
		"https://api.bfl.ai/v1/get_result?a=1#frag": "https://api.bfl.ai/v1/get_result?a=1&id=req+1#frag",
	} {
		got, err := ensurePollingURLHasID(in, "req 1")
		if err != nil || got != want {
			t.Errorf("ensurePollingURLHasID(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestImageTooManyInputImagesMessageMatchesTS(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k"}), "flux-kontext-pro")
	files := make([]provider.ImageFile, 11)
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", Files: files})
	if err == nil || err.Error() != "Black Forest Labs supports up to 10 input images." {
		t.Fatalf("err = %v", err)
	}
}
