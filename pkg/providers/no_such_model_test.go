package providers_test

import (
	"errors"
	"reflect"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/assemblyai"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	"github.com/digitallysavvy/go-ai/pkg/providers/bfl"
	"github.com/digitallysavvy/go-ai/pkg/providers/bytedance"
	"github.com/digitallysavvy/go-ai/pkg/providers/cartesia"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepgram"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/elevenlabs"
	"github.com/digitallysavvy/go-ai/pkg/providers/fal"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/fishaudio"
	"github.com/digitallysavvy/go-ai/pkg/providers/gladia"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/heygen"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/hume"
	"github.com/digitallysavvy/go-ai/pkg/providers/klingai"
	"github.com/digitallysavvy/go-ai/pkg/providers/lmnt"
	"github.com/digitallysavvy/go-ai/pkg/providers/luma"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/prodia"
	"github.com/digitallysavvy/go-ai/pkg/providers/quiverai"
	"github.com/digitallysavvy/go-ai/pkg/providers/replicate"
	"github.com/digitallysavvy/go-ai/pkg/providers/revai"
	"github.com/digitallysavvy/go-ai/pkg/providers/stability"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/topaz"
	"github.com/digitallysavvy/go-ai/pkg/providers/typesafeai"
	"github.com/digitallysavvy/go-ai/pkg/providers/voyage"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// TestUnsupportedModelKindsReturnNoSuchModelError pins that providers
// without a given model kind return *NoSuchModelError (TS throws
// NoSuchModelError({ modelId, modelType })), so errors.As works.
func TestUnsupportedModelKindsReturnNoSuchModelError(t *testing.T) {
	type kind struct {
		method, modelType, wantID, wantMsg string
	}
	cases := []struct {
		name  string
		prov  func() any
		kinds []kind
	}{
		{"alibaba", func() any { return alibaba.New(alibaba.Config{APIKey: "k"}) }, []kind{
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"assemblyai", func() any { return assemblyai.New(assemblyai.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "unknown", "AssemblyAI does not provide language models"},
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"azure", func() any { return mustNew(azure.New(azure.Config{APIKey: "k"})) }, []kind{
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"bfl", func() any { return bfl.New(bfl.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", ""},
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"bytedance", func() any { return mustNew(bytedance.New(bytedance.Config{APIKey: "k"})) }, []kind{
			{"LanguageModel", "languageModel", "m1", ""},
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"cartesia", func() any { return cartesia.New(cartesia.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", "Cartesia does not provide language models"},
			{"EmbeddingModel", "embeddingModel", "m1", "Cartesia does not provide embedding models"},
			{"ImageModel", "imageModel", "m1", "Cartesia does not provide image models"},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"cohere", func() any { return cohere.New(cohere.Config{APIKey: "k"}) }, []kind{
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
		}},
		{"deepgram", func() any { return deepgram.New(deepgram.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", "Deepgram does not provide language models"},
			{"EmbeddingModel", "embeddingModel", "m1", "Deepgram does not provide text embedding models"},
			{"ImageModel", "imageModel", "m1", "Deepgram does not provide image models"},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"deepseek", func() any { return deepseek.New(deepseek.Config{APIKey: "k"}) }, []kind{
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"elevenlabs", func() any { return elevenlabs.New(elevenlabs.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", "ElevenLabs does not provide language models"},
			{"EmbeddingModel", "embeddingModel", "m1", "ElevenLabs does not provide embedding models"},
			{"ImageModel", "imageModel", "m1", "ElevenLabs does not provide image models"},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"fal", func() any { return fal.New(fal.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", ""},
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"fireworks", func() any { return fireworks.New(fireworks.Config{APIKey: "k"}) }, []kind{
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"fishaudio", func() any { return fishaudio.New(fishaudio.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", "Fish Audio does not provide language models"},
			{"EmbeddingModel", "embeddingModel", "m1", "Fish Audio does not provide embedding models"},
			{"ImageModel", "imageModel", "m1", "Fish Audio does not provide image models"},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"gladia", func() any { return gladia.New(gladia.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", "Gladia does not provide language models"},
			{"EmbeddingModel", "embeddingModel", "m1", "Gladia does not provide embedding models"},
			{"ImageModel", "imageModel", "m1", "Gladia does not provide image models"},
			{"SpeechModel", "speechModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"gmicloud", func() any { return gmicloud.New(gmicloud.Config{APIKey: "k"}) }, []kind{
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"groq", func() any { return groq.New(groq.Config{APIKey: "k"}) }, []kind{
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"heygen", func() any { return heygen.New(heygen.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", ""},
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"huggingface", func() any { return huggingface.New(huggingface.Config{APIKey: "k"}) }, []kind{
			{"EmbeddingModel", "embeddingModel", "m1", "Hugging Face Responses API does not support text embeddings. Use the Hugging Face Inference API directly for embeddings."},
			{"ImageModel", "imageModel", "m1", "Hugging Face Responses API does not support image generation. Use the Hugging Face Inference API directly for image models."},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"hume", func() any { return hume.New(hume.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", "Hume does not provide language models"},
			{"EmbeddingModel", "embeddingModel", "m1", "Hume does not provide embedding models"},
			{"ImageModel", "imageModel", "m1", "Hume does not provide image models"},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"klingai", func() any { return mustNew(klingai.New(klingai.Config{APIKey: "k"})) }, []kind{
			{"LanguageModel", "languageModel", "m1", ""},
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"lmnt", func() any { return lmnt.New(lmnt.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", ""},
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"ImageModel", "imageModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"luma", func() any { return luma.New(luma.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", ""},
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"mistral", func() any { return mistral.New(mistral.Config{APIKey: "k"}) }, []kind{
			{"ImageModel", "imageModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"moonshot", func() any { return moonshot.New(moonshot.Config{APIKey: "k"}) }, []kind{
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"ollama", func() any { return ollama.New(ollama.Config{}) }, []kind{
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"openai", func() any { return openai.New(openai.Config{APIKey: "k"}) }, []kind{
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"openresponses", func() any { return openresponses.New(openresponses.Config{APIKey: "k", BaseURL: "http://x"}) }, []kind{
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"perplexity", func() any { return perplexity.New(perplexity.Config{APIKey: "k"}) }, []kind{
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"prodia", func() any { return prodia.New(prodia.Config{APIKey: "k"}) }, []kind{
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"quiverai", func() any { return quiverai.New(quiverai.Config{APIKey: "k"}) }, []kind{
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"replicate", func() any { return replicate.New(replicate.Config{APIKey: "k"}) }, []kind{
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"revai", func() any { return revai.New(revai.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "unknown", "Rev.ai does not provide language models"},
			{"EmbeddingModel", "embeddingModel", "unknown", "Rev.ai does not provide text embedding models"},
			{"ImageModel", "imageModel", "unknown", "Rev.ai does not provide image models"},
			{"SpeechModel", "speechModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"stability", func() any { return stability.New(stability.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", ""},
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"together", func() any { return together.New(together.Config{APIKey: "k"}) }, []kind{
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
		}},
		{"topaz", func() any { return topaz.New(topaz.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", ""},
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"typesafeai", func() any { return typesafeai.New(typesafeai.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", ""},
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"voyage", func() any { return voyage.New(voyage.Config{APIKey: "k"}) }, []kind{
			{"LanguageModel", "languageModel", "m1", ""},
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
		}},
		{"xai", func() any { return xai.New(xai.Config{APIKey: "k"}) }, []kind{
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
		{"zai", func() any { return zai.New(zai.Config{APIKey: "k"}) }, []kind{
			{"EmbeddingModel", "embeddingModel", "m1", ""},
			{"ImageModel", "imageModel", "m1", ""},
			{"SpeechModel", "speechModel", "m1", ""},
			{"TranscriptionModel", "transcriptionModel", "m1", ""},
			{"RerankingModel", "rerankingModel", "m1", ""},
		}},
	}
	for _, c := range cases {
		for _, k := range c.kinds {
			t.Run(c.name+"/"+k.method, func(t *testing.T) {
				res := reflect.ValueOf(c.prov()).MethodByName(k.method).Call([]reflect.Value{reflect.ValueOf("m1")})
				errv := res[len(res)-1]
				if errv.IsNil() {
					t.Fatalf("%s: expected an error", k.method)
				}
				err := errv.Interface().(error)
				var nsm *providererrors.NoSuchModelError
				if !errors.As(err, &nsm) {
					t.Fatalf("%s: error %T %v is not *NoSuchModelError", k.method, err, err)
				}
				if nsm.ModelType != k.modelType || nsm.ModelID != k.wantID {
					t.Errorf("%s: got {%q,%q}, want {%q,%q}", k.method, nsm.ModelID, nsm.ModelType, k.wantID, k.modelType)
				}
				if k.wantMsg != "" && err.Error() != k.wantMsg {
					t.Errorf("%s: message %q, want %q", k.method, err.Error(), k.wantMsg)
				}
				if k.wantMsg == "" && err.Error() != "No such "+k.modelType+": "+k.wantID {
					t.Errorf("%s: default message %q", k.method, err.Error())
				}
				if !errors.Is(err, providererrors.ErrModelNotFound) {
					t.Errorf("%s: errors.Is(ErrModelNotFound) must stay true", k.method)
				}
			})
		}
	}
}

func mustNew[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
