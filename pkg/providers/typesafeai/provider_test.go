package typesafeai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ported from packages/typesafe-ai/src/typesafe-ai-provider.test.ts.

func TestProvider_ProvidesEvaluationCapability(t *testing.T) {
	p := New(Config{})
	model, err := p.EvaluationModel("jev-latest")
	if err != nil {
		t.Fatalf("EvaluationModel: %v", err)
	}
	if model.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion = %q", model.SpecificationVersion())
	}
	if model.Provider() != "typesafe.evaluation" {
		t.Fatalf("Provider = %q", model.Provider())
	}
	if model.ModelID() != "jev-latest" {
		t.Fatalf("ModelID = %q", model.ModelID())
	}
	want := []string{"choice", "score", "boolean"}
	got := model.SupportedQuestionTypes()
	if len(got) != len(want) {
		t.Fatalf("SupportedQuestionTypes = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SupportedQuestionTypes = %v", got)
		}
	}
}

func TestProvider_RejectsUnsupportedFactories(t *testing.T) {
	p := New(Config{})
	if _, err := p.LanguageModel("unknown"); err == nil {
		t.Fatal("LanguageModel should be unsupported")
	}
	if _, err := p.EmbeddingModel("unknown"); err == nil {
		t.Fatal("EmbeddingModel should be unsupported")
	}
	if _, err := p.ImageModel("unknown"); err == nil {
		t.Fatal("ImageModel should be unsupported")
	}
	if _, err := p.SpeechModel("unknown"); err == nil {
		t.Fatal("SpeechModel should be unsupported")
	}
	if _, err := p.TranscriptionModel("unknown"); err == nil {
		t.Fatal("TranscriptionModel should be unsupported")
	}
	if _, err := p.RerankingModel("unknown"); err == nil {
		t.Fatal("RerankingModel should be unsupported")
	}
}

func TestCreateTypeSafeAI(t *testing.T) {
	p := CreateTypeSafeAI(Config{APIKey: "k"})
	if p.Name() != "typesafe" {
		t.Fatalf("Name = %q", p.Name())
	}
}

func TestNew_TrailingSlashIsStripped(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(nativeResponseJSON))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL + "/"})
	model, _ := p.EvaluationModel("jev-latest")
	_, err := model.DoEvaluate(t.Context(), provider.EvaluationCallOptions{State: "test", Questions: nativeQuestions()})
	if err != nil {
		t.Fatalf("DoEvaluate: %v", err)
	}
	if gotPath != "/systemone" {
		t.Fatalf("path = %q, want /systemone (no double slash)", gotPath)
	}
}
