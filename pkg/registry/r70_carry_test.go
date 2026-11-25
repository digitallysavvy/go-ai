package registry

import (
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS NoSuchProviderError lists Object.keys(providers): array-index names
// first in ascending numeric order, then the other names in insertion order.
func TestR70ProviderListUsesObjectKeysOrder(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	for _, name := range []string{"zeta", "10", "alpha", "2", "007", "4294967295", "4294967294", "-1", "1.5", "0"} {
		r.RegisterProvider(name, &testutil.MockProvider{})
	}
	want := []string{"0", "2", "10", "4294967294", "zeta", "alpha", "007", "4294967295", "-1", "1.5"}
	if got := r.ListProviders(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ListProviders = %v, want %v", got, want)
	}
	_, err := r.ResolveLanguageModel("missing:model")
	wantMsg := "No such provider: missing (available providers: 0,2,10,4294967294,zeta,alpha,007,4294967295,-1,1.5)"
	if err == nil || err.Error() != wantMsg {
		t.Fatalf("err = %v, want %q", err, wantMsg)
	}
	var nsp *NoSuchProviderError
	if !errors.As(err, &nsp) || !reflect.DeepEqual(nsp.AvailableProviders, want) {
		t.Fatalf("AvailableProviders = %+v", nsp)
	}
}

// r70IDProvider records the model ID each factory receives and returns a
// fixed model for every model type.
type r70IDProvider struct {
	testutil.MockProvider
	got      *[]string
	language provider.LanguageModel
	video    provider.VideoModelV3
	decision provider.DecisionModel
}

func newR70IDProvider() *r70IDProvider {
	got := &[]string{}
	rec := func(id string) { *got = append(*got, id) }
	p := &r70IDProvider{got: got, language: &testutil.MockLanguageModel{ModelName: "lm"}, video: registryVideoModel{}, decision: &mockDecisionModel{provider: "p", modelID: "d"}}
	embedding := &testutil.MockEmbeddingModel{ModelName: "em"}
	image := &testutil.MockImageModel{ModelName: "im"}
	speech := &testutil.MockSpeechModel{ModelName: "sm"}
	transcription := &testutil.MockTranscriptionModel{ModelName: "tm"}
	reranking := &testutil.MockRerankingModel{ModelName: "rm"}
	p.LanguageModelFunc = func(id string) (provider.LanguageModel, error) { rec(id); return p.language, nil }
	p.EmbeddingModelFunc = func(id string) (provider.EmbeddingModel, error) { rec(id); return embedding, nil }
	p.ImageModelFunc = func(id string) (provider.ImageModel, error) { rec(id); return image, nil }
	p.SpeechModelFunc = func(id string) (provider.SpeechModel, error) { rec(id); return speech, nil }
	p.TranscriptionModelFunc = func(id string) (provider.TranscriptionModel, error) { rec(id); return transcription, nil }
	p.RerankingModelFunc = func(id string) (provider.RerankingModel, error) { rec(id); return reranking, nil }
	return p
}

func (p *r70IDProvider) VideoModel(id string) (provider.VideoModelV3, error) {
	*p.got = append(*p.got, id)
	return p.video, nil
}

func (p *r70IDProvider) DecisionModel(id string) (provider.DecisionModel, error) {
	*p.got = append(*p.got, id)
	return p.decision, nil
}

func r70Resolvers(r *Registry) []struct {
	modelType string
	fn        func(string) (interface{}, error)
} {
	return []struct {
		modelType string
		fn        func(string) (interface{}, error)
	}{
		{"languageModel", func(id string) (interface{}, error) { return r.ResolveLanguageModel(id) }},
		{"embeddingModel", func(id string) (interface{}, error) { return r.ResolveEmbeddingModel(id) }},
		{"imageModel", func(id string) (interface{}, error) { return r.ResolveImageModel(id) }},
		{"transcriptionModel", func(id string) (interface{}, error) { return r.ResolveTranscriptionModel(id) }},
		{"speechModel", func(id string) (interface{}, error) { return r.ResolveSpeechModel(id) }},
		{"rerankingModel", func(id string) (interface{}, error) { return r.ResolveRerankingModel(id) }},
		{"videoModel", func(id string) (interface{}, error) { return r.ResolveVideoModel(id) }},
		{"decisionModel", func(id string) (interface{}, error) { return r.ResolveDecisionModel(id) }},
	}
}

// TS provider-registry.test.ts "should return ... model from provider",
// "with additional colon" and "should support custom separator (with
// multiple characters)" for every model type, plus decision-model.test.ts
// "preserves ... colons inside model IDs" / "custom separators": only the
// first separator splits; the rest of the ID reaches the factory unchanged.
func TestR70RegistryForwardsModelIDAfterFirstSeparator(t *testing.T) {
	t.Parallel()
	cases := []struct {
		sep, id, wantModelID string
	}{
		{":", "provider:model", "model"},
		{":", "provider:model:part2", "model:part2"},
		{"|", "provider|model", "model"},
		{" > ", "provider > model", "model"},
		{"::", "provider::model::version", "model::version"},
	}
	for _, c := range cases {
		p := newR70IDProvider()
		r := NewRegistry(WithSeparator(c.sep))
		r.RegisterProvider("provider", p)
		for _, res := range r70Resolvers(r) {
			*p.got = nil
			got, err := res.fn(c.id)
			if err != nil {
				t.Fatalf("sep %q %s(%q): %v", c.sep, res.modelType, c.id, err)
			}
			if got == nil {
				t.Fatalf("sep %q %s(%q): nil model", c.sep, res.modelType, c.id)
			}
			if !reflect.DeepEqual(*p.got, []string{c.wantModelID}) {
				t.Fatalf("sep %q %s(%q): factory got %v, want [%q]", c.sep, res.modelType, c.id, *p.got, c.wantModelID)
			}
		}
		// Identity: the language and decision models come back unchanged (no
		// middleware configured; TS toEqual(model) / toBe(model)).
		if lm, _ := r.ResolveLanguageModel(c.id); lm != p.language {
			t.Fatalf("sep %q: language model not returned as is", c.sep)
		}
		if dm, _ := r.ResolveDecisionModel(c.id); dm != p.decision {
			t.Fatalf("sep %q: decision model not returned as is", c.sep)
		}
	}
}

// TS "should throw NoSuchModelError if model id doesn't contain a colon"
// (every model type) and decision "reports malformed IDs": the message names
// the model type, the ID and the configured separator.
func TestR70RegistryMalformedIDPerModelType(t *testing.T) {
	t.Parallel()
	for _, sep := range []string{":", " > "} {
		r := NewRegistry(WithSeparator(sep))
		r.RegisterProvider("provider", newR70IDProvider())
		for _, res := range r70Resolvers(r) {
			got, err := res.fn("model")
			if got != nil {
				t.Fatalf("%s: got a model for a malformed ID", res.modelType)
			}
			var nsm *providererrors.NoSuchModelError
			if !errors.As(err, &nsm) || nsm.ModelID != "model" || nsm.ModelType != res.modelType {
				t.Fatalf("%s: err = %v (%+v)", res.modelType, err, nsm)
			}
			var nsp *NoSuchProviderError
			if errors.As(err, &nsp) {
				t.Fatalf("%s: malformed ID must not be a NoSuchProviderError", res.modelType)
			}
			want := "Invalid " + res.modelType + " id for registry: model (must be in the format \"providerId" + sep + "modelId\")"
			if err.Error() != want {
				t.Fatalf("%s: message = %q, want %q", res.modelType, err.Error(), want)
			}
		}
	}
}

// TS "should throw NoSuchProviderError if provider does not exist" for every
// model type, with an empty registry: the ID after the provider is ignored
// and the available list is empty.
func TestR70RegistryEmptyRegistryNoSuchProvider(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	for _, res := range r70Resolvers(r) {
		_, err := res.fn("provider:model:part2")
		var nsp *NoSuchProviderError
		if !errors.As(err, &nsp) {
			t.Fatalf("%s: err = %v, want *NoSuchProviderError", res.modelType, err)
		}
		if nsp.ProviderID != "provider" || nsp.ModelID != "provider" || nsp.ModelType != res.modelType || len(nsp.AvailableProviders) != 0 {
			t.Fatalf("%s: got %+v", res.modelType, nsp)
		}
		if want := "No such provider: provider (available providers: )"; err.Error() != want {
			t.Fatalf("%s: message = %q, want %q", res.modelType, err.Error(), want)
		}
	}
}
