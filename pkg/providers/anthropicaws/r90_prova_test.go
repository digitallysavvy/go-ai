package anthropicaws

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS anthropic-aws-provider.ts getRegion / getHeaders: a missing region (no
// baseURL) or workspace ID is loadSetting's LoadSettingError.
func TestR90provaMissingSettingsAreLoadSettingErrors(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("ANTHROPIC_AWS_WORKSPACE_ID", "")
	for _, c := range []struct {
		cfg  Config
		want string
	}{
		{Config{WorkspaceID: "w"}, "AWS region setting is missing. Pass it using the 'region' parameter or the AWS_REGION environment variable."},
		{Config{Region: "us-east-1"}, "Anthropic AWS workspace ID setting is missing. Pass it using the 'workspaceId' parameter or the ANTHROPIC_AWS_WORKSPACE_ID environment variable."},
	} {
		_, err := New(c.cfg)
		var lse *providererrors.LoadSettingError
		if !errors.As(err, &lse) || err.Error() != c.want {
			t.Errorf("err = %v, want %s", err, c.want)
		}
	}
}

// TS anthropic-aws-provider.ts apiKey?: Resolvable<string>
// (createApiKeyFetchFunction): a function key is resolved per request, sent
// as x-api-key without SigV4, and must be a non-empty string.
func TestR90provaAPIKeyFunc(t *testing.T) {
	var gotKey, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotAuth = r.Header.Get("x-api-key"), r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","id":"m","model":"x","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()
	key := "fn-key"
	p, err := New(Config{Region: "us-east-1", WorkspaceID: "w", BaseURL: srv.URL, HTTPClient: srv.Client(), AccessKeyID: "AKID", SecretAccessKey: "s",
		APIKeyFunc: func(context.Context) (string, error) { return key, nil }})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := p.LanguageModel("claude-sonnet-4-6")
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if gotKey != "fn-key" || gotAuth != "" {
		t.Errorf("x-api-key = %q, Authorization = %q", gotKey, gotAuth)
	}
	key = "  "
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var lke *providererrors.LoadAPIKeyError
	if !errors.As(err, &lke) || lke.Message != "Anthropic AWS API key must be a non-empty string." {
		t.Errorf("err = %v", err)
	}
}
