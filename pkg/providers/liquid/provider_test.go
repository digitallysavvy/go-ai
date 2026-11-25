package liquid

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func refundCall() provider.DecisionCallOptions {
	return provider.DecisionCallOptions{
		State:     []provider.DecisionStatePart{provider.DecisionTextPart("Refund me.")},
		Questions: map[string]provider.DecisionQuestion{"refund": {Type: "boolean", Instructions: "Refund request?"}},
	}
}

func TestProviderRejectsUnsupportedModelTypes(t *testing.T) {
	p := New(Config{})
	if p.Name() != "liquid" {
		t.Fatalf("name = %q", p.Name())
	}
	checks := map[string]func() error{
		"languageModel":      func() error { _, err := p.LanguageModel("other"); return err },
		"embeddingModel":     func() error { _, err := p.EmbeddingModel("other"); return err },
		"imageModel":         func() error { _, err := p.ImageModel("other"); return err },
		"speechModel":        func() error { _, err := p.SpeechModel("other"); return err },
		"transcriptionModel": func() error { _, err := p.TranscriptionModel("other"); return err },
		"rerankingModel":     func() error { _, err := p.RerankingModel("other"); return err },
	}
	for name, check := range checks {
		var noSuch *providererrors.NoSuchModelError
		if err := check(); !errors.As(err, &noSuch) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// TS "uses explicit or environment keys and the default endpoint".
func TestProviderUsesExplicitOrEnvironmentKeysAndDefaultEndpoint(t *testing.T) {
	for _, explicit := range []bool{true, false} {
		t.Setenv("LIQUID_API_KEY", "env-key")
		var gotURL, gotAuth, gotUA, gotBody string
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(r.Body)
			gotURL, gotAuth, gotUA, gotBody = r.URL.String(), r.Header.Get("Authorization"), r.Header.Get("User-Agent"), string(raw)
			return &http.Response{
				StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"answers":{"refund":{"type":"noul","noul":0.9}}}`)),
			}, nil
		})}
		cfg := Config{HTTPClient: client}
		want := "Bearer env-key"
		if explicit {
			cfg.APIKey, want = "explicit-key", "Bearer explicit-key"
		}
		m, err := New(cfg).DecisionModel("d1.2-ultra")
		if err != nil {
			t.Fatal(err)
		}
		res, err := m.DoDecide(context.Background(), refundCall())
		if err != nil {
			t.Fatal(err)
		}
		if res.Usage != nil || res.Response.ModelID != "d1.2-ultra" {
			t.Fatalf("usage=%v model=%s", res.Usage, res.Response.ModelID)
		}
		if gotURL != "https://api.liquid.ai/decisions/v1/systemone" || gotAuth != want || !strings.Contains(gotUA, "ai-sdk-liquid/"+version.Version) {
			t.Fatalf("url=%s auth=%s ua=%s", gotURL, gotAuth, gotUA)
		}
		if !strings.Contains(gotBody, `"model":"d1.2-ultra"`) {
			t.Fatalf("body = %s", gotBody)
		}
	}
}

func TestProviderLoadsCredentialsLazilyAndReportsMissingKey(t *testing.T) {
	t.Setenv("LIQUID_API_KEY", "")
	m, err := New(Config{}).DecisionModel("d1.2")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoDecide(context.Background(), refundCall())
	if err == nil || !strings.Contains(err.Error(), "LIQUID_API_KEY") || !providererrors.IsLoadAPIKeyError(err) {
		t.Fatalf("err = %v", err)
	}
}
