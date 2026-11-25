package anthropic

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Credential transports (S2, round 88 sweep): the bearer token or SigV4
// signature goes only to the Bedrock runtime origin; a cross-origin
// redirect hop receives none.
func TestR88provaBedrockAnthropicCredentialsSameOriginOnly(t *testing.T) {
	for _, mode := range []string{"bearer", "sigv4"} {
		base, foreign, api := r88provaRedirectServers(t)
		cfg := Config{BaseURL: base, Region: "us-east-1", Headers: map[string]string{"X-Custom-Header": "v"}}
		if mode == "bearer" {
			cfg.BearerToken = "tok"
		} else {
			t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
			cfg.Credentials = &AWSCredentials{AccessKeyID: "AKID", SecretAccessKey: "secret", SessionToken: "st"}
		}
		m, err := New(cfg).LanguageModel("anthropic.claude-sonnet-4-5-20250929-v1:0")
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

// TS 6b101ac076: Bedrock Anthropic rejects message threads before a request.
func TestR88provaBedrockAnthropicRejectsThreads(t *testing.T) {
	m, _ := New(Config{BaseURL: "http://127.0.0.1:1", Region: "us-east-1", BearerToken: "tok"}).LanguageModel("anthropic.claude-sonnet-4-5-20250929-v1:0")
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thread": map[string]interface{}{"type": "create"}}}})
	if err == nil || err.Error() != "bedrock.anthropic.messages does not support Anthropic message threads." {
		t.Fatalf("err = %v", err)
	}
}

// TS createAmazonBedrockAnthropicFetch parses the error body with
// safeParseJSON (secureJsonParse): a body with a __proto__ key does not
// parse, so the whole text is the message.
func TestR88provaBedrockErrorBodySecureParse(t *testing.T) {
	body := `{"__proto__":{"x":1},"message":"m"}`
	want := `{"type":"error","error":{"type":"error","message":"{\"__proto__\":{\"x\":1},\"message\":\"m\"}"}}`
	if got := string(transformErrorBody([]byte(body))); got != want {
		t.Fatalf("got %s", got)
	}
	if got := string(transformErrorBody([]byte(`{"message":"m","x":1}`))); got != `{"type":"error","error":{"type":"error","message":"m"}}` {
		t.Fatalf("got %s", got)
	}
}
