package mantle

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Credential transports (S2, round 88 sweep): the bearer token or SigV4
// signature goes only to the Mantle origin; a cross-origin redirect hop
// receives none.
func TestR88provaMantleCredentialsSameOriginOnly(t *testing.T) {
	for _, mode := range []string{"bearer", "sigv4"} {
		base, foreign, api := r88provaRedirectServers(t)
		s := ProviderSettings{BaseURL: base + "/v1", Region: "us-east-1"}
		if mode == "bearer" {
			s.APIKey = "tok"
		} else {
			t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
			s.AccessKeyID, s.SecretAccessKey, s.SessionToken = "AKID", "secret", "st"
		}
		m, err := New(s).Chat("openai.gpt-oss-120b-1:0")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if len(api()) != 1 || api()[0].Get("Authorization") == "" {
			t.Fatalf("%s: API request headers = %v", mode, api())
		}
		if len(foreign()) != 1 {
			t.Fatalf("%s: foreign requests = %d", mode, len(foreign()))
		}
		for _, h := range []string{"Authorization", "X-Amz-Date", "X-Amz-Security-Token"} {
			if v := foreign()[0].Get(h); v != "" {
				t.Fatalf("%s: foreign origin received %s: %q", mode, h, v)
			}
		}
	}
}
