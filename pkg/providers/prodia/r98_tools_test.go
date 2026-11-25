package prodia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS prodia-language-model.ts warns when `options.stopSequences !==
// undefined`: an empty array is still a set option. Go checked len > 0 and
// sent no warning for an empty slice (review round 98; the class other
// providers already follow, "TS != null: an empty array counts").
func TestR98toolsLanguageEmptyStopSequencesWarns(t *testing.T) {
	body, ct := buildTestMultipartBody(`{"id":"job-stop-1"}`, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A}, "image/png")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	model := NewLanguageModel(New(Config{APIKey: "test-key", BaseURL: srv.URL}), LanguageModelNanoBananaImgToImgV2)
	for _, tc := range []struct {
		name string
		stop []string
		want bool
	}{{"nil", nil, false}, {"empty", []string{}, true}, {"set", []string{"x"}, true}} {
		result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
			Prompt:        types.Prompt{Text: "a cat"},
			StopSequences: tc.stop,
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := false
		for _, w := range result.Warnings {
			if w.Type == "unsupported" && w.Feature == "stopSequences" {
				got = true
			}
		}
		if got != tc.want {
			t.Errorf("%s: stopSequences warning = %v, want %v", tc.name, got, tc.want)
		}
	}
}
