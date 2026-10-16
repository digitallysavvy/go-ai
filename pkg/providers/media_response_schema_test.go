package providers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	"github.com/digitallysavvy/go-ai/pkg/providers/cartesia"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/elevenlabs"
	"github.com/digitallysavvy/go-ai/pkg/providers/fal"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/fishaudio"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/quiverai"
	"github.com/digitallysavvy/go-ai/pkg/providers/revai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/voyage"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

func embedCall(m provider.EmbeddingModel, err error) func() error {
	return func() error {
		if err != nil {
			return err
		}
		_, e := m.DoEmbedMany(context.Background(), []string{"a"}, nil)
		return e
	}
}

func imageCall(m provider.ImageModel, err error) func() error {
	return func() error {
		if err != nil {
			return err
		}
		_, e := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "cat"})
		return e
	}
}

func transcribeCall(m provider.TranscriptionModel, err error) func() error {
	return func() error {
		if err != nil {
			return err
		}
		_, e := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"})
		return e
	}
}

func rerankCall(m provider.RerankingModel, err error) func() error {
	return func() error {
		if err != nil {
			return err
		}
		_, e := m.DoRerank(context.Background(), &provider.RerankOptions{Query: "q", Documents: []string{"a"}})
		return e
	}
}

// TS createJsonResponseHandler(schema): a 2xx embedding, image, reranking or
// transcription body that does not match the provider's response schema is an
// APICallError "Invalid JSON response" (status and body attached); the AI
// Gateway turns it into a GatewayResponseError like asGatewayError.
func TestMediaModelResponseSchemaValidation(t *testing.T) {
	const wrong = `{"unexpected":true}`
	type tc struct {
		name string
		body string // overrides wrong
		call func(base string) func() error
	}
	cases := []tc{
		{"openai/embedding", "", func(u string) func() error {
			return embedCall(openai.New(openai.Config{BaseURL: u, APIKey: "k"}).EmbeddingModel("m"))
		}},
		{"openai/image", "", func(u string) func() error {
			return imageCall(openai.New(openai.Config{BaseURL: u, APIKey: "k"}).ImageModel("m"))
		}},
		{"openai/transcription", "", func(u string) func() error {
			return transcribeCall(openai.New(openai.Config{BaseURL: u, APIKey: "k"}).TranscriptionModel("m"))
		}},
		{"azure/embedding", "", func(u string) func() error {
			p, err := azure.New(azure.Config{BaseURL: u, APIKey: "k"})
			if err != nil {
				return func() error { return err }
			}
			return embedCall(p.EmbeddingModel("m"))
		}},
		{"mistral/embedding", "", func(u string) func() error {
			return embedCall(mistral.New(mistral.Config{BaseURL: u, APIKey: "k"}).EmbeddingModel("m"))
		}},
		{"mistral/transcription", "", func(u string) func() error {
			return transcribeCall(mistral.New(mistral.Config{BaseURL: u, APIKey: "k"}).TranscriptionModel("m"))
		}},
		{"cohere/embedding", "", func(u string) func() error {
			return embedCall(cohere.New(cohere.Config{BaseURL: u, APIKey: "k"}).EmbeddingModel("m"))
		}},
		{"cohere/rerank", "", func(u string) func() error {
			return rerankCall(cohere.New(cohere.Config{BaseURL: u, APIKey: "k"}).RerankingModel("m"))
		}},
		{"voyage/embedding", "", func(u string) func() error {
			return embedCall(voyage.New(voyage.Config{BaseURL: u, APIKey: "k"}).EmbeddingModel("m"))
		}},
		{"voyage/rerank", "", func(u string) func() error {
			return rerankCall(voyage.New(voyage.Config{BaseURL: u, APIKey: "k"}).RerankingModel("m"))
		}},
		{"perplexity/embedding", "", func(u string) func() error {
			return embedCall(perplexity.New(perplexity.Config{BaseURL: u, APIKey: "k"}).EmbeddingModel("m"))
		}},
		{"alibaba/embedding", "", func(u string) func() error {
			return embedCall(alibaba.New(alibaba.Config{EmbeddingBaseURL: u, APIKey: "k"}).EmbeddingModel("m"))
		}},
		{"together/embedding", "", func(u string) func() error {
			return embedCall(together.New(together.Config{BaseURL: u, APIKey: "k"}).EmbeddingModel("m"))
		}},
		{"together/image", "", func(u string) func() error {
			return imageCall(together.New(together.Config{BaseURL: u, APIKey: "k"}).ImageModel("m"))
		}},
		{"together/rerank", "", func(u string) func() error {
			return rerankCall(together.New(together.Config{BaseURL: u, APIKey: "k"}).RerankingModel("m"))
		}},
		{"fireworks/embedding", "", func(u string) func() error {
			return embedCall(fireworks.New(fireworks.Config{BaseURL: u, APIKey: "k"}).EmbeddingModel("m"))
		}},
		{"fireworks/image-async", "", func(u string) func() error {
			return imageCall(fireworks.New(fireworks.Config{BaseURL: u, APIKey: "k"}).ImageModel("accounts/fireworks/models/flux-kontext-pro"))
		}},
		{"deepinfra/image", "", func(u string) func() error {
			return imageCall(deepinfra.New(deepinfra.Config{BaseURL: u, APIKey: "k"}).ImageModel("m"))
		}},
		{"xai/image", "", func(u string) func() error {
			return imageCall(xai.New(xai.Config{BaseURL: u, APIKey: "k"}).ImageModel("m"))
		}},
		{"xai/transcription", "", func(u string) func() error {
			return transcribeCall(xai.New(xai.Config{BaseURL: u, APIKey: "k"}).TranscriptionModel("m"))
		}},
		{"groq/transcription", "", func(u string) func() error {
			return transcribeCall(groq.New(groq.Config{BaseURL: u, APIKey: "k"}).TranscriptionModel("m"))
		}},
		{"elevenlabs/transcription", "", func(u string) func() error {
			return transcribeCall(elevenlabs.New(elevenlabs.Config{BaseURL: u, APIKey: "k"}).TranscriptionModel("scribe_v1"))
		}},
		{"fishaudio/transcription", "", func(u string) func() error {
			return transcribeCall(fishaudio.New(fishaudio.Config{BaseURL: u, APIKey: "k"}).TranscriptionModel("m"))
		}},
		{"revai/transcription", `{"id":1}`, func(u string) func() error {
			return transcribeCall(revai.New(revai.Config{BaseURL: u, APIKey: "k"}).TranscriptionModel("m"))
		}},
		{"cartesia/transcription", "", func(u string) func() error {
			return transcribeCall(cartesia.New(cartesia.Config{BaseURL: u, APIKey: "k"}).TranscriptionModel("ink-whisper"))
		}},
		{"cartesia/streaming-token", `{"token":""}`, func(u string) func() error {
			return func() error {
				m, err := cartesia.New(cartesia.Config{BaseURL: u, APIKey: "k"}).TranscriptionModel("ink-2")
				if err != nil {
					return err
				}
				rate := 16000
				_, err = m.(interface {
					DoStream(context.Context, *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error)
				}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
					Audio: silentAudio{}, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate}})
				return err
			}
		}},
		{"quiverai/image", "", func(u string) func() error {
			return imageCall(quiverai.New(quiverai.Config{BaseURL: u, APIKey: "k"}).ImageModel("m"))
		}},
		{"fal/image", "", func(u string) func() error {
			return imageCall(fal.New(fal.Config{BaseURL: u, APIKey: "k"}).ImageModel("fal-ai/flux"))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := wrong
			if c.body != "" {
				body = c.body
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			err := c.call(srv.URL)()
			var pe *providererrors.ProviderError
			if !errors.As(err, &pe) || pe.Message != "Invalid JSON response" {
				t.Fatalf("error = %v, want ProviderError \"Invalid JSON response\"", err)
			}
			if pe.StatusCode != 200 || pe.ResponseBody != body {
				t.Fatalf("status/body not carried: %d %q", pe.StatusCode, pe.ResponseBody)
			}
		})
	}
}

func TestGatewayMediaResponseSchemaValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"unexpected":true}`))
	}))
	defer srv.Close()
	p, err := gateway.New(gateway.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]func() error{
		"embedding":     embedCall(p.EmbeddingModel("m")),
		"image":         imageCall(p.ImageModel("m")),
		"reranking":     rerankCall(p.RerankingModel("m")),
		"transcription": transcribeCall(p.TranscriptionModel("m")),
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			var re *gatewayerrors.GatewayResponseError
			if !errors.As(err, &re) {
				t.Fatalf("error = %T %v, want GatewayResponseError", err, err)
			}
			if re.GetStatusCode() != 200 {
				t.Fatalf("status = %d", re.GetStatusCode())
			}
		})
	}
}
