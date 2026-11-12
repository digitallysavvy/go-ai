package bedrock

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r92provaAuthServer answers Converse requests and records each request's
// Authorization header.
func r92provaAuthServer(t *testing.T) (*httptest.Server, func() string) {
	t.Helper()
	var mu sync.Mutex
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		auth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"output":{"message":{"role":"assistant","content":[{"text":"ok"}]}},"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() string {
		mu.Lock()
		defer mu.Unlock()
		return auth
	}
}

// TS createAmazonBedrock reads AWS_BEARER_TOKEN_BEDROCK once, when the
// provider is created (loadOptionalSetting, amazon-bedrock-provider.ts:198-212),
// and picks bearer or SigV4 authentication then.
func TestR92provaBearerTokenEnvReadAtCreation(t *testing.T) {
	srv, lastAuth := r92provaAuthServer(t)
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKID")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	generate := func(p *Provider) string {
		t.Helper()
		lm, err := p.LanguageModel("anthropic.claude-3-haiku-20240307-v1:0")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
			t.Fatal(err)
		}
		return lastAuth()
	}

	p := New(Config{Region: "us-east-1", BaseURL: srv.URL})
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "late-token")
	if auth := generate(p); !strings.HasPrefix(auth, "AWS4-HMAC-SHA256") {
		t.Fatalf("Authorization = %q, want SigV4 (token set after creation)", auth)
	}
	p = New(Config{Region: "us-east-1", BaseURL: srv.URL})
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	if auth := generate(p); auth != "Bearer late-token" {
		t.Fatalf("Authorization = %q, want the token read at creation", auth)
	}
}
