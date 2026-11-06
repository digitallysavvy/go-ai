package providers_test

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropicaws"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	bedrockanthropic "github.com/digitallysavvy/go-ai/pkg/providers/bedrock/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock/mantle"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
	vertexanthropic "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex/anthropic"
	vertexxai "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/minimax"
)

type allModelFactories interface {
	EmbeddingModel(string) (provider.EmbeddingModel, error)
	ImageModel(string) (provider.ImageModel, error)
	SpeechModel(string) (provider.SpeechModel, error)
	TranscriptionModel(string) (provider.TranscriptionModel, error)
	RerankingModel(string) (provider.RerankingModel, error)
}

// TS providers throw NoSuchModelError({modelId, modelType}) for every model
// type they do not offer; Go callers match it with errors.As.
func TestAnthropicGoogleFamiliesUnsupportedModelTypesReturnNoSuchModelError(t *testing.T) {
	vtx, err := googlevertex.New(googlevertex.Config{Project: "p", Location: "us-central1", AccessToken: "t"})
	must(t, err)
	vxai, err := vertexxai.New(vertexxai.Config{Project: "p", Location: "global", AccessToken: "t"})
	must(t, err)
	aws, err := anthropicaws.New(anthropicaws.Config{Region: "us-east-1", WorkspaceID: "w", APIKey: "k"})
	must(t, err)

	type unsupported struct {
		p     allModelFactories
		types []string
	}
	all := []string{"embeddingModel", "imageModel", "speechModel", "transcriptionModel", "rerankingModel"}
	cases := map[string]unsupported{
		"anthropic":         {anthropic.New(anthropic.Config{APIKey: "k"}), all},
		"anthropicaws":      {aws, all},
		"google":            {google.New(google.Config{APIKey: "k"}), []string{"rerankingModel"}},
		"googlevertex":      {vtx, []string{"rerankingModel"}},
		"vertex-maas":       {googlevertex.NewMaaS(googlevertex.MaaSConfig{}), []string{"speechModel", "transcriptionModel", "rerankingModel"}},
		"vertex-anthropic":  {vertexanthropic.New(vertexanthropic.Options{}), all},
		"vertex-xai":        {vxai, all},
		"bedrock":           {bedrock.New(bedrock.Config{Region: "us-east-1"}), []string{"speechModel", "transcriptionModel"}},
		"bedrock-anthropic": {bedrockanthropic.New(bedrockanthropic.Config{}), all},
		"bedrock-mantle":    {mantle.New(mantle.ProviderSettings{}), all},
		"minimax":           {minimax.New(minimax.Config{APIKey: "k"}), all},
	}
	call := func(p allModelFactories, typ string) error {
		var err error
		switch typ {
		case "embeddingModel":
			_, err = p.EmbeddingModel("bad-id")
		case "imageModel":
			_, err = p.ImageModel("bad-id")
		case "speechModel":
			_, err = p.SpeechModel("bad-id")
		case "transcriptionModel":
			_, err = p.TranscriptionModel("bad-id")
		case "rerankingModel":
			_, err = p.RerankingModel("bad-id")
		}
		return err
	}
	for name, c := range cases {
		for _, typ := range c.types {
			err := call(c.p, typ)
			var nsm *providererrors.NoSuchModelError
			if !errors.As(err, &nsm) {
				t.Errorf("%s.%s error = %v (%T), want *NoSuchModelError", name, typ, err, err)
				continue
			}
			if nsm.ModelID != "bad-id" || nsm.ModelType != typ {
				t.Errorf("%s.%s = %+v", name, typ, nsm)
			}
			if want := "No such " + typ + ": bad-id"; err.Error() != want {
				t.Errorf("%s.%s message = %q, want %q", name, typ, err.Error(), want)
			}
			if !errors.Is(err, providererrors.ErrModelNotFound) {
				t.Errorf("%s.%s should still match ErrModelNotFound", name, typ)
			}
		}
	}
}
