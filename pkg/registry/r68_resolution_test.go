package registry

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// nilVideoProvider has a video factory that returns no model.
type nilVideoProvider struct{ testutil.MockProvider }

func (nilVideoProvider) VideoModel(string) (provider.VideoModelV3, error) { return nil, nil }

// nilDecisionProvider has a decision factory that returns no model.
type nilDecisionProvider struct{ testutil.MockProvider }

func (nilDecisionProvider) DecisionModel(string) (provider.DecisionModel, error) { return nil, nil }

func wantRegistryNoSuchModel(t *testing.T, name string, err error, id, modelType string) {
	t.Helper()
	var nsm *providererrors.NoSuchModelError
	if !errors.As(err, &nsm) {
		t.Fatalf("%s: err = %v (%T), want *NoSuchModelError", name, err, err)
	}
	var nsp *NoSuchProviderError
	if errors.As(err, &nsp) {
		t.Fatalf("%s: err = %T, must not be a NoSuchProviderError", name, err)
	}
	if nsm.ModelID != id || nsm.ModelType != modelType {
		t.Fatalf("%s: got {%q %q}, want {%q %q}", name, nsm.ModelID, nsm.ModelType, id, modelType)
	}
	if want := "No such " + modelType + ": " + id; err.Error() != want {
		t.Fatalf("%s: message = %q, want %q", name, err.Error(), want)
	}
}

// TS provider-registry.ts: a provider whose factory returns null throws
// NoSuchModelError({ modelId: id, modelType }) with the full registry id
// ("should throw NoSuchModelError if provider does not return a model").
func TestR68RegistryNilModelIsNoSuchModelError(t *testing.T) {
	t.Parallel()
	nilProvider := &testutil.MockProvider{
		LanguageModelFunc:      func(string) (provider.LanguageModel, error) { return nil, nil },
		EmbeddingModelFunc:     func(string) (provider.EmbeddingModel, error) { return nil, nil },
		ImageModelFunc:         func(string) (provider.ImageModel, error) { return nil, nil },
		SpeechModelFunc:        func(string) (provider.SpeechModel, error) { return nil, nil },
		TranscriptionModelFunc: func(string) (provider.TranscriptionModel, error) { return nil, nil },
		RerankingModelFunc:     func(string) (provider.RerankingModel, error) { return nil, nil },
	}
	r := NewRegistry(WithSeparator("::"))
	r.RegisterProvider("p", nilProvider)
	r.RegisterProvider("v", &nilVideoProvider{})
	r.RegisterProvider("d", &nilDecisionProvider{})
	r.RegisterAlias("alias", "p::m")

	cases := []struct {
		name, id, modelType string
		fn                  func() (interface{}, error)
	}{
		{"language", "p::m", "languageModel", func() (interface{}, error) { return r.ResolveLanguageModel("p::m") }},
		{"language alias", "p::m", "languageModel", func() (interface{}, error) { return r.ResolveLanguageModel("alias") }},
		{"embedding", "p::m", "embeddingModel", func() (interface{}, error) { return r.ResolveEmbeddingModel("p::m") }},
		{"image", "p::m", "imageModel", func() (interface{}, error) { return r.ResolveImageModel("p::m") }},
		{"speech", "p::m", "speechModel", func() (interface{}, error) { return r.ResolveSpeechModel("p::m") }},
		{"transcription", "p::m", "transcriptionModel", func() (interface{}, error) { return r.ResolveTranscriptionModel("p::m") }},
		{"reranking", "p::m", "rerankingModel", func() (interface{}, error) { return r.ResolveRerankingModel("p::m") }},
		{"video", "v::m", "videoModel", func() (interface{}, error) { return r.ResolveVideoModel("v::m") }},
		{"decision", "d::m", "decisionModel", func() (interface{}, error) { return r.ResolveDecisionModel("d::m") }},
		{"evaluation", "d::m", "decisionModel", func() (interface{}, error) { return r.ResolveEvaluationModel("d::m") }},
	}
	for _, c := range cases {
		got, err := c.fn()
		if got != nil {
			t.Fatalf("%s: got model %v, want nil", c.name, got)
		}
		wantRegistryNoSuchModel(t, c.name, err, c.id, c.modelType)
	}
}

// TS: provider.videoModel?.(modelId) / missing decisionModel factory is
// undefined -> NoSuchModelError, not NoSuchProviderError ("reports missing
// decision capabilities or models").
func TestR68RegistryMissingFactoryIsNoSuchModelError(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.RegisterProvider("provider", &testutil.MockProvider{})

	_, err := r.ResolveVideoModel("provider:model")
	wantRegistryNoSuchModel(t, "video", err, "provider:model", "videoModel")
	_, err = r.ResolveDecisionModel("provider:model")
	wantRegistryNoSuchModel(t, "decision", err, "provider:model", "decisionModel")
	_, err = r.ResolveEvaluationModel("provider:model")
	wantRegistryNoSuchModel(t, "evaluation", err, "provider:model", "decisionModel")
}

// TS decision-model.test.ts "identifies unknown providers with the existing
// marker-based error": NoSuchProviderError.isInstance and
// NoSuchModelError.isInstance are both true; getProvider sets modelId to the
// provider id.
func TestR68UnknownProviderIsAlsoNoSuchModelError(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.RegisterProvider("known", &testutil.MockProvider{})

	resolvers := map[string]func() error{
		"languageModel":      func() error { _, err := r.ResolveLanguageModel("missing:model"); return err },
		"embeddingModel":     func() error { _, err := r.ResolveEmbeddingModel("missing:model"); return err },
		"imageModel":         func() error { _, err := r.ResolveImageModel("missing:model"); return err },
		"speechModel":        func() error { _, err := r.ResolveSpeechModel("missing:model"); return err },
		"transcriptionModel": func() error { _, err := r.ResolveTranscriptionModel("missing:model"); return err },
		"rerankingModel":     func() error { _, err := r.ResolveRerankingModel("missing:model"); return err },
		"videoModel":         func() error { _, err := r.ResolveVideoModel("missing:model"); return err },
		"decisionModel":      func() error { _, err := r.ResolveDecisionModel("missing:model"); return err },
	}
	for modelType, fn := range resolvers {
		err := fn()
		var nsp *NoSuchProviderError
		if !errors.As(err, &nsp) {
			t.Fatalf("%s: err = %T, want *NoSuchProviderError", modelType, err)
		}
		if nsp.ProviderID != "missing" || nsp.ModelID != "missing" || nsp.ModelType != modelType ||
			len(nsp.AvailableProviders) != 1 || nsp.AvailableProviders[0] != "known" {
			t.Fatalf("%s: got %+v", modelType, nsp)
		}
		var nsm *providererrors.NoSuchModelError
		if !errors.As(err, &nsm) {
			t.Fatalf("%s: errors.As(*NoSuchModelError) = false, want true", modelType)
		}
		want := "No such provider: missing (available providers: known)"
		if nsm.ModelID != "missing" || nsm.ModelType != modelType || nsm.Error() != want {
			t.Fatalf("%s: NoSuchModelError view = %+v (%q)", modelType, nsm, nsm.Error())
		}
	}

	// Files/Skills: unknown provider uses getProvider(id, 'languageModel');
	// the unsupported-capability error is a plain Error in TS.
	_, err := r.Files("missing")
	var nsp *NoSuchProviderError
	if !errors.As(err, &nsp) || nsp.ModelType != "languageModel" || nsp.ModelID != "missing" {
		t.Fatalf("Files(missing) = %v (%+v)", err, nsp)
	}
	_, err = r.Skills("known")
	if !errors.As(err, &nsp) || nsp.ModelType != "languageModel" || nsp.ModelID != "known" {
		t.Fatalf("Skills(known) = %v (%+v)", err, nsp)
	}
	if providererrors.IsNoSuchModelError(err) {
		t.Fatal("unsupported-skills error must not match NoSuchModelError (TS plain Error)")
	}
}

// Provider factory errors pass through unchanged (TS "preserves errors thrown
// by provider factories").
func TestR68RegistryFactoryErrorPassesThrough(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	r := NewRegistry()
	r.RegisterProvider("p", &testutil.MockProvider{
		LanguageModelFunc: func(string) (provider.LanguageModel, error) { return nil, boom },
	})
	if _, err := r.ResolveLanguageModel("p:m"); err != boom {
		t.Fatalf("err = %v, want boom", err)
	}
}
