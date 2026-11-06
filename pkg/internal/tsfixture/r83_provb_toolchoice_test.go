package tsfixture_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// TestR83provbUnknownToolChoiceType: with tools, a tool choice type TS's
// exhaustive prepareTools switch does not know throws
// UnsupportedFunctionalityError "tool choice type: X" (openai chat and
// Responses, azure, xai Responses, alibaba, moonshot, openai-compatible
// providers), huggingface throws a plain Error, deepseek and zai warn and send
// none, and open-responses sends the type as given.
func TestR83provbUnknownToolChoiceType(t *testing.T) {
	var mu sync.Mutex
	var last []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		last = b
		mu.Unlock()
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"message":"x"}}`))
	}))
	defer srv.Close()
	az, _ := azure.New(azure.Config{APIKey: "k", BaseURL: srv.URL})
	const unsupported = "'tool choice type: unrecognized' functionality not supported."
	mk := []struct {
		n       string
		f       func() (provider.LanguageModel, error)
		wantErr string // "" = the request is sent
		wantTC  string // tool_choice JSON in the body, "" = none
	}{
		{"openai-chat", func() (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}).ChatModel("gpt-4o")
		}, unsupported, ""},
		{"openai-resp", func() (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-4o")
		}, unsupported, ""},
		{"azure-resp", func() (provider.LanguageModel, error) { return az.ResponsesModel("gpt-4o") }, unsupported, ""},
		{"xai", func() (provider.LanguageModel, error) {
			return xai.New(xai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("grok-4")
		}, unsupported, ""},
		{"deepseek", func() (provider.LanguageModel, error) {
			return deepseek.New(deepseek.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("deepseek-chat")
		}, "", ""},
		{"alibaba", func() (provider.LanguageModel, error) {
			return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("qwen-plus")
		}, unsupported, ""},
		{"moonshot", func() (provider.LanguageModel, error) {
			return moonshot.New(moonshot.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("kimi-k2.5")
		}, unsupported, ""},
		{"openresp", func() (provider.LanguageModel, error) {
			return openresponses.New(openresponses.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		}, "", `"tool_choice":"unrecognized"`},
		{"hf", func() (provider.LanguageModel, error) {
			return huggingface.New(huggingface.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		}, "Unsupported tool choice type: [object Object]", ""},
		{"fireworks", func() (provider.LanguageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		}, unsupported, ""},
		{"together", func() (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		}, unsupported, ""},
		{"gmicloud", func() (provider.LanguageModel, error) {
			return gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		}, unsupported, ""},
		{"ollama", func() (provider.LanguageModel, error) {
			return ollama.New(ollama.Config{BaseURL: srv.URL}).LanguageModel("m")
		}, unsupported, ""},
		{"zai", func() (provider.LanguageModel, error) {
			return zai.New(zai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("glm-4.6")
		}, "", ""},
	}
	for _, c := range mk {
		m, err := c.f()
		if err != nil {
			t.Fatal(c.n, err)
		}
		mu.Lock()
		last = nil
		mu.Unlock()
		_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
			Tools:      []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"}}},
			ToolChoice: types.ToolChoice{Type: "unrecognized"}})
		mu.Lock()
		body := string(last)
		mu.Unlock()
		if c.wantErr != "" {
			var uf *providererrors.UnsupportedFunctionalityError
			if err == nil || err.Error() != c.wantErr || (c.wantErr == unsupported && !errors.As(err, &uf)) {
				t.Errorf("%s: err = %v, want %q", c.n, err, c.wantErr)
			}
			if body != "" {
				t.Errorf("%s: request sent: %s", c.n, body)
			}
			continue
		}
		if c.wantTC == "" && strings.Contains(body, `"tool_choice"`) || c.wantTC != "" && !strings.Contains(body, c.wantTC) {
			t.Errorf("%s: body %s, want tool_choice %q", c.n, body, c.wantTC)
		}
	}
}
