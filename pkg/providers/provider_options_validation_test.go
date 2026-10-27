package providers_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/voyage"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// An invalid provider option fails the call before any request is sent, like
// TS parseProviderOptions (InvalidArgumentError "invalid <provider> provider
// options"). The models below point at an unroutable host: reaching the
// network would surface a different error.
func TestChatProviderOptionsInvalidArgument(t *testing.T) {
	gm, _ := groq.New(groq.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}).LanguageModel("llama")
	am, _ := alibaba.New(alibaba.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}).LanguageModel("qwen")
	mm, _ := mistral.New(mistral.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}).LanguageModel("small")
	cm, _ := cohere.New(cohere.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}).LanguageModel("command")
	zm, _ := zai.New(zai.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}).LanguageModel("glm")
	dm, _ := deepseek.New(deepseek.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}).LanguageModel("chat")
	cases := []struct {
		name     string
		model    provider.LanguageModel
		key      string
		bad      map[string]interface{}
		wantText string
	}{
		{"groq enum", gm, "groq", map[string]interface{}{"reasoningEffort": "extreme"}, "invalid groq provider options"},
		{"groq tier", gm, "groq", map[string]interface{}{"serviceTier": "gold"}, "invalid groq provider options"},
		{"alibaba budget", am, "alibaba", map[string]interface{}{"thinkingBudget": 0}, "invalid alibaba provider options"},
		{"alibaba bool", am, "alibaba", map[string]interface{}{"enableThinking": "yes"}, "invalid alibaba provider options"},
		{"mistral enum", mm, "mistral", map[string]interface{}{"reasoningEffort": "medium"}, "invalid mistral provider options"},
		{"mistral type", mm, "mistral", map[string]interface{}{"safePrompt": 1}, "invalid mistral provider options"},
		{"cohere thinking", cm, "cohere", map[string]interface{}{"thinking": map[string]interface{}{"type": "maybe"}}, "invalid cohere provider options"},
		{"zai id", zm, "zai", map[string]interface{}{"requestId": "abc"}, "invalid zai provider options"},
		{"zai effort", zm, "zai", map[string]interface{}{"reasoningEffort": "extreme"}, "invalid zai provider options"},
		{"deepseek user", dm, "deepseek", map[string]interface{}{"userId": "bad id!"}, "invalid deepseek provider options"},
		{"deepseek top", dm, "deepseek", map[string]interface{}{"topLogprobs": 21}, "invalid deepseek provider options"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := &provider.GenerateOptions{
				Prompt:          types.Prompt{Text: "hi"},
				ProviderOptions: map[string]interface{}{c.key: c.bad},
			}
			for _, call := range []func() error{
				func() error { _, err := c.model.DoGenerate(context.Background(), opts); return err },
				func() error { _, err := c.model.DoStream(context.Background(), opts); return err },
			} {
				err := call()
				var invalid *providererrors.InvalidArgumentError
				if !errors.As(err, &invalid) || invalid.Field != "providerOptions" || !strings.Contains(invalid.Message, c.wantText) || invalid.Cause == nil {
					t.Fatalf("error = %#v, want InvalidArgumentError %q", err, c.wantText)
				}
			}
		})
	}
}

func TestEmbeddingProviderOptionsInvalidArgument(t *testing.T) {
	mm, _ := mistral.New(mistral.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}).EmbeddingModel("mistral-embed")
	vm, _ := voyage.New(voyage.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}).EmbeddingModel("voyage-3")
	for name, c := range map[string]struct {
		model provider.EmbeddingModel
		key   string
		bad   map[string]interface{}
	}{
		"mistral dtype":     {mm, "mistral", map[string]interface{}{"outputDtype": "float64"}},
		"mistral dimension": {mm, "mistral", map[string]interface{}{"outputDimension": 0}},
		"voyage input":      {vm, "voyage", map[string]interface{}{"inputType": "other"}},
		"voyage trunc":      {vm, "voyage", map[string]interface{}{"truncation": "yes"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := c.model.DoEmbedMany(context.Background(), []string{"x"}, &provider.EmbedModelOptions{
				ProviderOptions: map[string]interface{}{c.key: c.bad},
			})
			var invalid *providererrors.InvalidArgumentError
			if !errors.As(err, &invalid) || invalid.Message != "invalid "+c.key+" provider options" {
				t.Fatalf("error = %#v, want InvalidArgumentError", err)
			}
		})
	}
	// null is accepted where the schema says nullable (voyage inputType).
	if _, err := vm.DoEmbedMany(context.Background(), []string{"x"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"voyage": map[string]interface{}{"inputType": nil}},
	}); err != nil {
		var invalid *providererrors.InvalidArgumentError
		if errors.As(err, &invalid) {
			t.Fatalf("nullable inputType rejected: %v", err)
		}
	}
}
