package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/assemblyai"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	"github.com/digitallysavvy/go-ai/pkg/providers/baseten"
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
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/hume"
	"github.com/digitallysavvy/go-ai/pkg/providers/luma"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/replicate"
	"github.com/digitallysavvy/go-ai/pkg/providers/revai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/topaz"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

type keyCase struct {
	name, env, description string
	header, prefix         string
	model                  func(url string, headers map[string]string) provider.LanguageModel
}

func chat(m provider.LanguageModel, _ error) provider.LanguageModel { return m }

func keyCases() []keyCase {
	return []keyCase{
		{"openai", "OPENAI_API_KEY", "OpenAI", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(openai.New(openai.Config{BaseURL: u, Headers: h}).ChatModel("m"))
		}},
		{"groq", "GROQ_API_KEY", "Groq", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(groq.New(groq.Config{BaseURL: u, Headers: h}).LanguageModel("m"))
		}},
		{"cohere", "COHERE_API_KEY", "Cohere", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(cohere.New(cohere.Config{BaseURL: u, Headers: h}).LanguageModel("m"))
		}},
		{"mistral", "MISTRAL_API_KEY", "Mistral", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(mistral.New(mistral.Config{BaseURL: u, Headers: h}).LanguageModel("m"))
		}},
		{"deepseek", "DEEPSEEK_API_KEY", "DeepSeek API key", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(deepseek.New(deepseek.Config{BaseURL: u, Headers: h}).LanguageModel("m"))
		}},
		{"xai", "XAI_API_KEY", "xAI API key", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(xai.New(xai.Config{BaseURL: u, Headers: h}).LanguageModel("m"))
		}},
		{"together", "TOGETHER_API_KEY", "TogetherAI", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(together.New(together.Config{BaseURL: u, Headers: h}).LanguageModel("m"))
		}},
		{"fireworks", "FIREWORKS_API_KEY", "Fireworks API key", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(fireworks.New(fireworks.Config{BaseURL: u, Headers: h}).LanguageModel("m"))
		}},
		{"alibaba", "ALIBABA_API_KEY", "Alibaba Cloud (DashScope)", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(alibaba.New(alibaba.Config{BaseURL: u, Headers: h}).LanguageModel("m"))
		}},
		{"moonshot", "MOONSHOT_API_KEY", "Moonshot API key", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(moonshot.New(moonshot.Config{BaseURL: u, Headers: h}).LanguageModel("m"))
		}},
		{"zai", "ZAI_API_KEY", "Z.AI API key", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(zai.New(zai.Config{BaseURL: u, Headers: h}).LanguageModel("m"))
		}},
		{"perplexity", "PERPLEXITY_API_KEY", "Perplexity", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(perplexity.New(perplexity.Config{BaseURL: u, Headers: h}).LanguageModel("m"))
		}},
		{"huggingface", "HUGGINGFACE_API_KEY", "Hugging Face", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(huggingface.New(huggingface.Config{BaseURL: u, Headers: h}).LanguageModel("m"))
		}},
		{"gmicloud", "GMI_CLOUD_APIKEY", "GMI Cloud API key", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(gmicloud.New(gmicloud.Config{BaseURL: u, Headers: h}).ChatModel("m"))
		}},
		{"baseten", "BASETEN_API_KEY", "Baseten API key", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(baseten.New(baseten.Config{BaseURL: u, Headers: h}).ChatModel("m"))
		}},
		{"cerebras", "CEREBRAS_API_KEY", "Cerebras API key", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(cerebras.New(cerebras.Config{BaseURL: u, Headers: h}).ChatModel("m"))
		}},
		{"deepinfra", "DEEPINFRA_API_KEY", "DeepInfra's API key", "Authorization", "Bearer ", func(u string, h map[string]string) provider.LanguageModel {
			return chat(deepinfra.New(deepinfra.Config{BaseURL: u, Headers: h}).LanguageModel("m"))
		}},
		{"azure", "AZURE_API_KEY", "Azure OpenAI", "api-key", "", func(u string, h map[string]string) provider.LanguageModel {
			p, err := azure.New(azure.Config{BaseURL: u, Headers: h})
			if err != nil {
				panic(err)
			}
			return chat(p.ChatModel("m"))
		}},
	}
}

// TS loadApiKey runs per request: a missing key fails the call with a
// LoadAPIKeyError before anything is sent, the environment variable is read
// at request time, an explicit credential header keeps working, and a
// wrapper never forwards OPENAI_API_KEY to another service.
func TestMissingAPIKeyErrorsAtRequestTime(t *testing.T) {
	for _, c := range keyCases() {
		t.Run(c.name, func(t *testing.T) {
			var requests atomic.Int32
			var got atomic.Value
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				got.Store(r.Header.Clone())
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer srv.Close()

			t.Setenv(c.env, "")
			t.Setenv("OPENAI_API_KEY", "")
			if c.name == "together" {
				t.Setenv("TOGETHER_AI_API_KEY", "")
			}
			opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}

			// No key anywhere: LoadAPIKeyError, nothing sent. The model is
			// built first, so the check also covers env-after-construction.
			model := c.model(srv.URL, nil)
			_, err := model.DoGenerate(context.Background(), opts)
			if !providererrors.IsLoadAPIKeyError(err) {
				t.Fatalf("DoGenerate error = %v, want LoadAPIKeyError", err)
			}
			if want := c.description + " API key is missing. Pass it using the 'apiKey' parameter or the " + c.env + " environment variable."; !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), want)
			}
			if n := requests.Load(); n != 0 {
				t.Fatalf("%d requests sent without an API key", n)
			}

			// The variable is read per request, after construction.
			t.Setenv(c.env, "late-key")
			_, _ = model.DoGenerate(context.Background(), opts)
			if h, _ := got.Load().(http.Header); h.Get(c.header) != c.prefix+"late-key" {
				t.Fatalf("%s = %q, want %q", c.header, h.Get(c.header), c.prefix+"late-key")
			}

			// Another service's key is never forwarded.
			if c.env != "OPENAI_API_KEY" {
				t.Setenv(c.env, "")
				t.Setenv("OPENAI_API_KEY", "openai-secret")
				requests.Store(0)
				_, err = model.DoGenerate(context.Background(), opts)
				if !providererrors.IsLoadAPIKeyError(err) || requests.Load() != 0 {
					t.Fatalf("OPENAI_API_KEY leaked to %s: err=%v requests=%d", c.name, err, requests.Load())
				}
			}

			// An explicit credential header is a credential (v0.5.2 setups).
			t.Setenv(c.env, "")
			t.Setenv("OPENAI_API_KEY", "")
			authed := c.model(srv.URL, map[string]string{c.header: c.prefix + "from-header"})
			_, err = authed.DoGenerate(context.Background(), opts)
			if providererrors.IsLoadAPIKeyError(err) {
				t.Fatalf("header-authenticated call failed with %v", err)
			}
			if h, _ := got.Load().(http.Header); h.Get(c.header) != c.prefix+"from-header" {
				t.Fatalf("%s = %q, want the caller header", c.header, h.Get(c.header))
			}
		})
	}
}

type clientKeyCase struct {
	name, env      string
	header, prefix string
	client         func(url string, headers map[string]string) *internalhttp.Client
}

// The speech, transcription, image and video providers load their key the
// same way (TS loadApiKey inside getHeaders).
func TestMissingAPIKeyErrorsAtRequestTimeNonChatProviders(t *testing.T) {
	cases := []clientKeyCase{
		{"cartesia", "CARTESIA_API_KEY", "Authorization", "Bearer ", func(u string, h map[string]string) *internalhttp.Client {
			return cartesia.New(cartesia.Config{BaseURL: u, Headers: h}).Client()
		}},
		{"gladia", "GLADIA_API_KEY", "x-gladia-key", "", func(u string, h map[string]string) *internalhttp.Client {
			return gladia.New(gladia.Config{BaseURL: u, Headers: h}).Client()
		}},
		{"hume", "HUME_API_KEY", "X-Hume-Api-Key", "", func(u string, h map[string]string) *internalhttp.Client {
			return hume.New(hume.Config{BaseURL: u, Headers: h}).Client()
		}},
		{"fishaudio", "FISH_AUDIO_API_KEY", "Authorization", "Bearer ", func(u string, h map[string]string) *internalhttp.Client {
			return fishaudio.New(fishaudio.Config{BaseURL: u, Headers: h}).Client()
		}},
		{"elevenlabs", "ELEVENLABS_API_KEY", "xi-api-key", "", func(u string, h map[string]string) *internalhttp.Client {
			return elevenlabs.New(elevenlabs.Config{BaseURL: u, Headers: h}).Client()
		}},
		{"deepgram", "DEEPGRAM_API_KEY", "Authorization", "Token ", func(u string, h map[string]string) *internalhttp.Client {
			return deepgram.New(deepgram.Config{BaseURL: u, Headers: h}).Client()
		}},
		{"assemblyai", "ASSEMBLYAI_API_KEY", "Authorization", "", func(u string, h map[string]string) *internalhttp.Client {
			return assemblyai.New(assemblyai.Config{BaseURL: u, Headers: h}).Client()
		}},
		{"revai", "REVAI_API_KEY", "Authorization", "Bearer ", func(u string, h map[string]string) *internalhttp.Client {
			return revai.New(revai.Config{BaseURL: u, Headers: h}).Client()
		}},
		{"luma", "LUMA_API_KEY", "Authorization", "Bearer ", func(u string, h map[string]string) *internalhttp.Client {
			return luma.New(luma.Config{BaseURL: u, Headers: h}).Client()
		}},
		{"topaz", "TOPAZ_API_KEY", "X-API-Key", "", func(u string, h map[string]string) *internalhttp.Client {
			return topaz.New(topaz.Config{BaseURL: u, Headers: h}).Client()
		}},
		{"replicate", "REPLICATE_API_TOKEN", "Authorization", "Bearer ", func(u string, h map[string]string) *internalhttp.Client {
			return replicate.New(replicate.Config{BaseURL: u, Headers: h}).Client()
		}},
		{"fal", "FAL_API_KEY", "Authorization", "Key ", func(u string, h map[string]string) *internalhttp.Client {
			return fal.New(fal.Config{BaseURL: u, Headers: h}).Client()
		}},
		{"bfl", "BFL_API_KEY", "X-Key", "", func(u string, h map[string]string) *internalhttp.Client {
			return bfl.New(bfl.Config{BaseURL: u, Headers: h}).Client()
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var requests atomic.Int32
			var got atomic.Value
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				got.Store(r.Header.Clone())
			}))
			defer srv.Close()
			t.Setenv(c.env, "")
			t.Setenv("FAL_KEY", "")

			client := c.client(srv.URL, nil)
			_, err := client.Get(context.Background(), "/x")
			if !providererrors.IsLoadAPIKeyError(err) || requests.Load() != 0 {
				t.Fatalf("err = %v, requests = %d, want LoadAPIKeyError and no request", err, requests.Load())
			}

			t.Setenv(c.env, "late-key")
			if _, err := client.Get(context.Background(), "/x"); err != nil {
				t.Fatal(err)
			}
			if h, _ := got.Load().(http.Header); h.Get(c.header) != c.prefix+"late-key" {
				t.Fatalf("%s = %q, want %q", c.header, h.Get(c.header), c.prefix+"late-key")
			}

			t.Setenv(c.env, "")
			authed := c.client(srv.URL, map[string]string{c.header: c.prefix + "from-header"})
			if _, err := authed.Get(context.Background(), "/x"); err != nil {
				t.Fatalf("header-authenticated call failed: %v", err)
			}
			if h, _ := got.Load().(http.Header); h.Get(c.header) != c.prefix+"from-header" {
				t.Fatalf("%s = %q, want the caller header", c.header, h.Get(c.header))
			}
		})
	}
}
