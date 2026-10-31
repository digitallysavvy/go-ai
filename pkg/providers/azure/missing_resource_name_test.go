package azure

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type countingTransport struct{ n atomic.Int32 }

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.n.Add(1)
	return nil, errors.New("no request should be sent")
}

// TS getResourceName (loadSetting) throws LoadSettingError when a request is
// built with neither baseURL nor resourceName / AZURE_RESOURCE_NAME; New
// itself never fails for it.
func TestMissingResourceNameIsLoadSettingErrorAtRequestTime(t *testing.T) {
	t.Setenv("AZURE_RESOURCE_NAME", "")
	rt := &countingTransport{}
	p, err := New(Config{APIKey: "k", HTTPClient: &http.Client{Transport: rt}})
	if err != nil {
		t.Fatalf("New must not fail: %v", err)
	}
	const want = "Azure OpenAI resource name setting is missing. Pass it using the 'resourceName' parameter or the AZURE_RESOURCE_NAME environment variable."
	check := func(name string, err error) {
		t.Helper()
		var lse *providererrors.LoadSettingError
		if !errors.As(err, &lse) || lse.Message != want {
			t.Errorf("%s: err = %v, want LoadSettingError %q", name, err, want)
		}
	}
	lm, err := p.LanguageModel("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	_, err = lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	check("language model", err)

	em, err := p.EmbeddingModel("text-embedding-3-small")
	if err != nil {
		t.Fatal(err)
	}
	_, err = em.DoEmbed(context.Background(), "hi", nil)
	check("embedding model", err)

	sm, err := p.SpeechModel("MAI-Voice-1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sm.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	check("speech (OpenAI API)", err)
	_, err = sm.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi", ProviderOptions: map[string]interface{}{"azure": map[string]interface{}{"api": "speech"}}})
	check("speech (Azure Speech API)", err)

	if rt.n.Load() != 0 {
		t.Fatalf("requests sent = %d, want 0", rt.n.Load())
	}

	// A baseURL makes the resource name unnecessary.
	p2, _ := New(Config{APIKey: "k", BaseURL: "https://proxy.example/openai", HTTPClient: &http.Client{Transport: rt}})
	lm2, _ := p2.LanguageModel("gpt-4o")
	if _, err := lm2.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); providererrors.IsLoadSettingError(err) {
		t.Fatalf("with BaseURL: err = %v", err)
	}
}
