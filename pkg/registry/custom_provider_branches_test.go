package registry

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type fallbackProvider struct {
	*testutil.MockProvider
	video  provider.VideoModelV3
	files  provider.FilesAPI
	skills provider.SkillsAPI
}

func (f *fallbackProvider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if f.video == nil {
		return nil, errors.New("no video")
	}
	return f.video, nil
}

func (f *fallbackProvider) Files() provider.FilesAPI { return f.files }
func (f *fallbackProvider) Skills() provider.SkillsAPI {
	return f.skills
}

func TestCustomProviderNameFallbackAndDefault(t *testing.T) {
	p := NewCustomProvider(CustomProviderOptions{
		Fallback: &testutil.MockProvider{ProviderName: "fallback"},
	})
	if p.Name() != "fallback" {
		t.Fatalf("Name() with fallback = %q, want fallback", p.Name())
	}

	plain := NewCustomProvider(CustomProviderOptions{})
	if plain.Name() != "custom" {
		t.Fatalf("Name() default = %q, want custom", plain.Name())
	}
}

func TestCustomProviderModelFallbackAndNoSuchModelErrors(t *testing.T) {
	video := registryVideoModel{}
	fallback := &fallbackProvider{
		MockProvider: &testutil.MockProvider{ProviderName: "fb"},
		video:        video,
	}

	withFallback := NewCustomProvider(CustomProviderOptions{Fallback: fallback})
	checks := []struct {
		name string
		fn   func() (interface{}, error)
	}{
		{"language", func() (interface{}, error) { return withFallback.LanguageModel("m") }},
		{"embedding", func() (interface{}, error) { return withFallback.EmbeddingModel("m") }},
		{"image", func() (interface{}, error) { return withFallback.ImageModel("m") }},
		{"speech", func() (interface{}, error) { return withFallback.SpeechModel("m") }},
		{"transcription", func() (interface{}, error) { return withFallback.TranscriptionModel("m") }},
		{"reranking", func() (interface{}, error) { return withFallback.RerankingModel("m") }},
		{"video", func() (interface{}, error) {
			return withFallback.(interface {
				VideoModel(string) (provider.VideoModelV3, error)
			}).VideoModel("m")
		}},
	}
	for _, c := range checks {
		got, err := c.fn()
		if err != nil || got == nil {
			t.Fatalf("%s fallback lookup failed: got=%v err=%v", c.name, got, err)
		}
	}

	noFallback := NewCustomProvider(CustomProviderOptions{})
	errChecks := []struct {
		name string
		fn   func() error
		want string
	}{
		{"language", func() error { _, err := noFallback.LanguageModel("m"); return err }, "no such languageModel: m"},
		{"embedding", func() error { _, err := noFallback.EmbeddingModel("m"); return err }, "no such embeddingModel: m"},
		{"image", func() error { _, err := noFallback.ImageModel("m"); return err }, "no such imageModel: m"},
		{"speech", func() error { _, err := noFallback.SpeechModel("m"); return err }, "no such speechModel: m"},
		{"transcription", func() error { _, err := noFallback.TranscriptionModel("m"); return err }, "no such transcriptionModel: m"},
		{"reranking", func() error { _, err := noFallback.RerankingModel("m"); return err }, "no such rerankingModel: m"},
		{"video", func() error {
			_, err := noFallback.(interface {
				VideoModel(string) (provider.VideoModelV3, error)
			}).VideoModel("m")
			return err
		}, "no such videoModel: m"},
	}
	for _, c := range errChecks {
		err := c.fn()
		if err == nil || err.Error() != c.want {
			t.Fatalf("%s no-fallback error = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestCustomProviderFilesAndSkillsSingleWrappers(t *testing.T) {
	files := registryFilesAPI{}
	skills := registrySkillsAPI{}

	filesOnly := NewCustomProvider(CustomProviderOptions{
		FilesFactory: func() provider.FilesAPI { return files },
	})
	fp, ok := filesOnly.(provider.FilesProvider)
	if !ok {
		t.Fatal("expected FilesProvider for files-only custom provider")
	}
	if fp.Files() == nil {
		t.Fatal("Files() should not be nil")
	}
	if _, ok := filesOnly.(provider.SkillsProvider); ok {
		t.Fatal("files-only custom provider should not implement SkillsProvider")
	}

	skillsOnly := NewCustomProvider(CustomProviderOptions{
		SkillsFactory: func() provider.SkillsAPI { return skills },
	})
	sp, ok := skillsOnly.(provider.SkillsProvider)
	if !ok {
		t.Fatal("expected SkillsProvider for skills-only custom provider")
	}
	if sp.Skills() == nil {
		t.Fatal("Skills() should not be nil")
	}
	if _, ok := skillsOnly.(provider.FilesProvider); ok {
		t.Fatal("skills-only custom provider should not implement FilesProvider")
	}
}
