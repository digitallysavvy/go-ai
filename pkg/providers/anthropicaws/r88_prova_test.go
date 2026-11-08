package anthropicaws

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Credential transports (S2, round 88 sweep): the x-api-key or SigV4
// signature goes only to the endpoint origin; a cross-origin redirect hop
// receives none.
func TestR88provaAnthropicAwsCredentialsSameOriginOnly(t *testing.T) {
	for _, mode := range []string{"apikey", "sigv4"} {
		base, foreign, api := r88provaRedirectServers(t)
		cfg := Config{BaseURL: base + "/v1", Region: "us-east-1", WorkspaceID: "ws"}
		if mode == "apikey" {
			cfg.APIKey = "tok"
		} else {
			t.Setenv("ANTHROPIC_AWS_API_KEY", "")
			cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken = "AKID", "secret", "st"
		}
		p, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		m, err := p.LanguageModel("claude-sonnet-4-5")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if len(api()) != 1 || (api()[0].Get("X-Api-Key") == "" && api()[0].Get("Authorization") == "") {
			t.Fatalf("%s: API request headers = %v", mode, api())
		}
		if len(foreign()) != 1 {
			t.Fatalf("%s: foreign requests = %d", mode, len(foreign()))
		}
		for _, h := range r88provaCredentialHeaders {
			if v := foreign()[0].Get(h); v != "" {
				t.Fatalf("%s: foreign origin received %s: %q", mode, h, v)
			}
		}
	}
}
