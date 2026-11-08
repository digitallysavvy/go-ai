package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// TestR88provbCompatRefusalTSCases replays the refusal tests of TS
// openai-compatible-chat-language-model.test.ts at 0f3a85f92d (646a7e3c2e:
// refusal text parts, content-filter finish, non-string refusals ignored)
// through every Go model built on the shared OpenAI-compatible chat base and
// the OpenAI chat model in openai-compatible mode. The three
// prepareJsonResponse cases carry the body that helper builds.
func TestR88provbCompatRefusalTSCases(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/compat_chat_r88.json.gz")
	if len(cases) != 10 {
		t.Fatalf("want 10 refusal cases, got %d", len(cases))
	}
	factories := map[string]func(base, id string) (provider.LanguageModel, error){
		"together": func(base, id string) (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: base}).LanguageModel(id)
		},
		"fireworks": func(base, id string) (provider.LanguageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: base}).LanguageModel(id)
		},
		"gmicloud": func(base, id string) (provider.LanguageModel, error) {
			return gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: base}).LanguageModel(id)
		},
		"zai": func(base, id string) (provider.LanguageModel, error) {
			return zai.New(zai.Config{APIKey: "k", BaseURL: base}).LanguageModel(id)
		},
		"ollama": func(base, id string) (provider.LanguageModel, error) {
			return ollama.New(ollama.Config{BaseURL: base}).LanguageModel(id)
		},
		"openai-compat": func(base, id string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: base, Name: "test-provider", OpenAICompatible: true}).ChatModel(id)
		},
	}
	keys := map[string]string{"together": "togetherai", "fireworks": "fireworks", "gmicloud": "gmicloud", "zai": "zai", "ollama": "ollama", "openai-compat": "test-provider"}
	for name, mk := range factories {
		mk := mk
		name := name
		t.Run(name, func(t *testing.T) {
			tsfixture.MetadataKeyAlias = map[string]string{"test-provider": keys[name]}
			defer func() { tsfixture.MetadataKeyAlias = nil }()
			tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
				if id == "" {
					id = "grok-beta"
				}
				m, err := mk(base, id)
				if err != nil {
					t.Fatal(err)
				}
				return m
			}, nil)
		})
	}
}

// TS 5f19ea5c61: an inline text file of a text/* media type is sent to an
// OpenAI-compatible provider as a text part instead of failing; other media
// types keep the "text file parts" error.
func TestR88provbCompatInlineTextFile(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"id":"c","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	m, err := fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("accounts/fireworks/models/x")
	if err != nil {
		t.Fatal(err)
	}
	msg := func(mediaType string) types.Prompt {
		return types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeText, Text: "Résumé: café"}},
		}}}}
	}
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: msg("text/plain")}); err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(body["messages"])
	if want := `[{"content":[{"text":"Résumé: café","type":"text"}],"role":"user"}]`; string(got) != want {
		t.Fatalf("messages = %s, want %s", got, want)
	}
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: msg("application/json")})
	var uf *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &uf) || uf.Functionality != "text file parts" {
		t.Fatalf("err = %v, want the text file parts error", err)
	}
}

// Model ID constants for together, fireworks and huggingface (TS model ID
// types at 0f3a85f92d; triage providers B §5).
func TestR88provbModelIDConstants(t *testing.T) {
	for got, want := range map[string]string{
		together.ModelMetaLlamaLlama3_3_70BInstructTurbo: "meta-llama/Llama-3.3-70B-Instruct-Turbo",
		fireworks.ModelKimiK2p6:                          "accounts/fireworks/models/kimi-k2p6",
		huggingface.ModelDeepseekAiDeepSeekV3_0324:       "deepseek-ai/DeepSeek-V3-0324",
	} {
		if got != want {
			t.Errorf("constant = %q, want %q", got, want)
		}
	}
}
