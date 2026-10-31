package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestBedrockAnthropicRegionResolvedLazilyFromEnv(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	p := New(Config{})
	t.Setenv("AWS_REGION", "eu-west-1")
	base, err := p.runtimeBaseURL()
	if err != nil || base != "https://bedrock-runtime.eu-west-1.amazonaws.com" {
		t.Fatalf("runtimeBaseURL = %q, %v", base, err)
	}
}

// With an explicit BaseURL no region is needed to build the model, but SigV4
// signing still needs one: TS loads the region before the credentials and
// fails the request with the loadSetting text instead of signing with an
// empty region.
func TestSigV4RequestWithoutRegionFailsLikeTS(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, Credentials: &AWSCredentials{AccessKeyID: "AKID", SecretAccessKey: "secret"}})
	model, err := p.LanguageModel("anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err == nil || !strings.Contains(err.Error(), "AWS region setting is missing. Pass it using the 'region' parameter or the AWS_REGION environment variable.") {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if hit {
		t.Fatal("request must not be sent unsigned-region")
	}
}
