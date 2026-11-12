package providers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/assemblyai"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	"github.com/digitallysavvy/go-ai/pkg/providers/baseten"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	"github.com/digitallysavvy/go-ai/pkg/providers/bfl"
	"github.com/digitallysavvy/go-ai/pkg/providers/cartesia"
	"github.com/digitallysavvy/go-ai/pkg/providers/cerebras"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepgram"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/elevenlabs"
	"github.com/digitallysavvy/go-ai/pkg/providers/fal"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/fishaudio"
	"github.com/digitallysavvy/go-ai/pkg/providers/gladia"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/hume"
	"github.com/digitallysavvy/go-ai/pkg/providers/lmnt"
	"github.com/digitallysavvy/go-ai/pkg/providers/luma"
	"github.com/digitallysavvy/go-ai/pkg/providers/minimax"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/prodia"
	"github.com/digitallysavvy/go-ai/pkg/providers/quiverai"
	"github.com/digitallysavvy/go-ai/pkg/providers/replicate"
	"github.com/digitallysavvy/go-ai/pkg/providers/revai"
	"github.com/digitallysavvy/go-ai/pkg/providers/stability"
	"github.com/digitallysavvy/go-ai/pkg/providers/tencent"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/topaz"
	"github.com/digitallysavvy/go-ai/pkg/providers/typesafeai"
	"github.com/digitallysavvy/go-ai/pkg/providers/voyage"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// PB-22 class sweep (cr-r91-provb-to-integrator 2, TS handle-fetch-error.ts):
// an aborted call (here: a cancelled context, no HTTP response) returns the
// abort error itself, never a status-0 *ProviderError, for every model type
// of these providers. The CR listed 14 sites; this probe found the class in
// every error handler that checks FetchRejectionCause (80 sites), which now
// also checks internalhttp.NoResponseError. A failed connection keeps its
// "Cannot connect to API" error (TS APICallError).
func TestR91integTransportErrorsUnchangedSweep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	azureP, _ := azure.New(azure.Config{APIKey: "k", BaseURL: srv.URL})
	providers := map[string]interface{}{
		"alibaba":       alibaba.New(alibaba.Config{APIKey: "k", BaseURL: srv.URL}),
		"anthropic":     anthropic.New(anthropic.Config{APIKey: "k", BaseURL: srv.URL}),
		"assemblyai":    assemblyai.New(assemblyai.Config{APIKey: "k", BaseURL: srv.URL}),
		"azure":         azureP,
		"bfl":           bfl.New(bfl.Config{APIKey: "k", BaseURL: srv.URL}),
		"cartesia":      cartesia.New(cartesia.Config{APIKey: "k", BaseURL: srv.URL}),
		"deepgram":      deepgram.New(deepgram.Config{APIKey: "k", BaseURL: srv.URL}),
		"deepseek":      deepseek.New(deepseek.Config{APIKey: "k", BaseURL: srv.URL}),
		"elevenlabs":    elevenlabs.New(elevenlabs.Config{APIKey: "k", BaseURL: srv.URL}),
		"fal":           fal.New(fal.Config{APIKey: "k", BaseURL: srv.URL}),
		"fishaudio":     fishaudio.New(fishaudio.Config{APIKey: "k", BaseURL: srv.URL}),
		"gladia":        gladia.New(gladia.Config{APIKey: "k", BaseURL: srv.URL}),
		"gmicloud":      gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: srv.URL}),
		"huggingface":   huggingface.New(huggingface.Config{APIKey: "k", BaseURL: srv.URL}),
		"hume":          hume.New(hume.Config{APIKey: "k", BaseURL: srv.URL}),
		"lmnt":          lmnt.New(lmnt.Config{APIKey: "k", BaseURL: srv.URL}),
		"luma":          luma.New(luma.Config{APIKey: "k", BaseURL: srv.URL}),
		"moonshot":      moonshot.New(moonshot.Config{APIKey: "k", BaseURL: srv.URL}),
		"openai":        openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}),
		"openresponses": openresponses.New(openresponses.Config{Name: "x", APIKey: "k", BaseURL: srv.URL}),
		"prodia":        prodia.New(prodia.Config{APIKey: "k", BaseURL: srv.URL}),
		"replicate":     replicate.New(replicate.Config{APIKey: "k", BaseURL: srv.URL}),
		"revai":         revai.New(revai.Config{APIKey: "k", BaseURL: srv.URL}),
		"stability":     stability.New(stability.Config{APIKey: "k", BaseURL: srv.URL}),
		"tencent":       tencent.New(tencent.Config{APIKey: "k", BaseURL: srv.URL}),
		"topaz":         topaz.New(topaz.Config{APIKey: "k", BaseURL: srv.URL}),
		"typesafeai":    typesafeai.New(typesafeai.Config{APIKey: "k", BaseURL: srv.URL}),
		"xai":           xai.New(xai.Config{APIKey: "k", BaseURL: srv.URL}),
		"zai":           zai.New(zai.Config{APIKey: "k", BaseURL: srv.URL}),
		"baseten":       baseten.New(baseten.Config{APIKey: "k", BaseURL: srv.URL}),
		"bedrock":       bedrock.New(bedrock.Config{APIKey: "k", Region: "us-east-1", BaseURL: srv.URL}),
		"cerebras":      cerebras.New(cerebras.Config{APIKey: "k", BaseURL: srv.URL}),
		"cohere":        cohere.New(cohere.Config{APIKey: "k", BaseURL: srv.URL}),
		"deepinfra":     deepinfra.New(deepinfra.Config{APIKey: "k", BaseURL: srv.URL}),
		"fireworks":     fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}),
		"google":        google.New(google.Config{APIKey: "k", BaseURL: srv.URL}),
		"groq":          groq.New(groq.Config{APIKey: "k", BaseURL: srv.URL}),
		"minimax":       minimax.New(minimax.Config{APIKey: "k", BaseURL: srv.URL, ImageBaseURL: srv.URL}),
		"mistral":       mistral.New(mistral.Config{APIKey: "k", BaseURL: srv.URL}),
		"perplexity":    perplexity.New(perplexity.Config{APIKey: "k", BaseURL: srv.URL}),
		"quiverai":      quiverai.New(quiverai.Config{APIKey: "k", BaseURL: srv.URL}),
		"together":      together.New(together.Config{APIKey: "k", BaseURL: srv.URL}),
		"voyage":        voyage.New(voyage.Config{APIKey: "k", BaseURL: srv.URL}),
	}
	prompt := types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}}
	var failures []string
	check := func(name string, err error) {
		var nsm *providererrors.NoSuchModelError
		if errors.As(err, &nsm) {
			return
		}
		var pe *providererrors.ProviderError
		if err == nil || !errors.Is(err, context.Canceled) || errors.As(err, &pe) {
			failures = append(failures, name+": "+errString(err))
		}
	}
	for name, p := range providers {
		if f, ok := p.(interface {
			LanguageModel(string) (provider.LanguageModel, error)
		}); ok {
			if m, err := f.LanguageModel("m"); err == nil {
				_, err = m.DoGenerate(ctx, &provider.GenerateOptions{Prompt: prompt})
				check(name+" language generate", err)
				_, err = m.DoStream(ctx, &provider.GenerateOptions{Prompt: prompt})
				check(name+" language stream", err)
			}
		}
		if f, ok := p.(interface {
			EmbeddingModel(string) (provider.EmbeddingModel, error)
		}); ok {
			if m, err := f.EmbeddingModel("m"); err == nil {
				_, err = m.DoEmbed(ctx, "x", &provider.EmbedModelOptions{})
				check(name+" embedding", err)
			}
		}
		if f, ok := p.(interface {
			ImageModel(string) (provider.ImageModel, error)
		}); ok {
			id := "m"
			if name == "google" {
				id = "gemini-2.5-flash-image" // Imagen IDs fail before any request
			}
			if m, err := f.ImageModel(id); err == nil {
				opts := &provider.ImageGenerateOptions{Prompt: "p"}
				if name == "topaz" { // Topaz enhances an input image
					opts.Files = []provider.ImageFile{{Type: "file", Data: []byte{0x89, 'P', 'N', 'G'}, MediaType: "image/png"}}
				}
				_, err = m.DoGenerate(ctx, opts)
				check(name+" image", err)
			}
		}
		if f, ok := p.(interface {
			SpeechModel(string) (provider.SpeechModel, error)
		}); ok {
			if m, err := f.SpeechModel("m"); err == nil {
				_, err = m.DoGenerate(ctx, &provider.SpeechGenerateOptions{Text: "hi", Voice: "v"})
				check(name+" speech", err)
			}
		}
		if f, ok := p.(interface {
			TranscriptionModel(string) (provider.TranscriptionModel, error)
		}); ok {
			// Cartesia transcribes only through its streaming model.
			if m, err := f.TranscriptionModel("m"); err == nil && name != "cartesia" {
				_, err = m.DoTranscribe(ctx, &provider.TranscriptionOptions{Audio: []byte("RIFF0000WAVE"), MimeType: "audio/wav"})
				check(name+" transcription", err)
			}
		}
		if f, ok := p.(interface {
			RerankingModel(string) (provider.RerankingModel, error)
		}); ok {
			if m, err := f.RerankingModel("m"); err == nil {
				_, err = m.DoRerank(ctx, &provider.RerankOptions{Documents: []string{"a"}, Query: "q"})
				check(name+" reranking", err)
			}
		}
	}
	rt := mistral.New(mistral.Config{APIKey: "k", BaseURL: srv.URL}).ExperimentalRealtime()
	_, err := rt.CreateSession(ctx, mistral.RealtimeCreateSessionOptions{Model: "m"})
	check("mistral realtime create", err)
	check("mistral realtime close", rt.CloseSession(ctx, "s1"))
	sort.Strings(failures)
	for _, f := range failures {
		t.Error(f)
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
