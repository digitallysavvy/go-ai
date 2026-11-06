package prodia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR83integProdiaTextOutputUsesTextDecoder: TS prodia-language-model.ts
// 409 decodes a text output part with new TextDecoder().decode(part.body).
func TestR83integProdiaTextOutputUsesTextDecoder(t *testing.T) {
	body, ct := buildTestMultipartBody(`{"id":"job-1"}`, []byte{0xEF, 0xBB, 0xBF, 0x68, 0x69, 0xF0, 0x9F, 0x98}, "text/plain")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), LanguageModelNanoBananaImgToImgV2)
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "a cat"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "hi\uFFFD"; result.Text != want {
		t.Fatalf("text = %q, want %q", result.Text, want)
	}
}

// TestR83integProdiaJobPartWithBOM: TS decodes the job part with new
// TextDecoder().decode before parseJSON, so a leading BOM is dropped.
func TestR83integProdiaJobPartWithBOM(t *testing.T) {
	body, ct := buildTestMultipartBody("\xEF\xBB\xBF"+`{"id":"job-bom"}`, []byte("hi"), "text/plain")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), LanguageModelNanoBananaImgToImgV2)
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "a cat"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "hi" {
		t.Fatalf("text = %q", result.Text)
	}
}
