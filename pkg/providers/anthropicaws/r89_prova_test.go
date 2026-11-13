package anthropicaws

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR89provaRegionLoadSettingError: the SigV4 fetch loads the region with
// loadSetting at request time, so a missing region is a LoadSettingError
// ("... environment variable."), even with a custom baseURL (TS
// anthropic-aws-provider.ts).
func TestR89provaRegionLoadSettingError(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("ANTHROPIC_AWS_API_KEY", "")
	p, err := New(Config{BaseURL: "http://127.0.0.1:1/v1", WorkspaceID: "ws", AccessKeyID: "AKID", SecretAccessKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.LanguageModel("claude-sonnet-4-5")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var lse *providererrors.LoadSettingError
	if !errors.As(err, &lse) || lse.Error() != "AWS region setting is missing. Pass it using the 'region' parameter or the AWS_REGION environment variable." {
		t.Fatalf("err = %T %v", err, err)
	}
}

// TestR89provaGenerateID: TS createAnthropicAws passes the generateId setting
// to the language model, which uses it for source ids (anthropic-aws-
// provider.ts).
func TestR89provaGenerateID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"x","content":[{"type":"text","text":"hi","citations":[{"type":"web_search_result_location","cited_text":"c","url":"https://a.example","title":"A","encrypted_index":"e"}]}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	p, err := New(Config{BaseURL: srv.URL, WorkspaceID: "ws", APIKey: "k", GenerateID: func() string { return "fixed-id" }})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := p.LanguageModel("claude-sonnet-4-5")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range res.Content {
		if s, ok := part.(types.SourceContent); ok {
			if s.ID != "fixed-id" {
				t.Fatalf("source id = %q", s.ID)
			}
			return
		}
	}
	t.Fatalf("no source in %+v", res.Content)
}
