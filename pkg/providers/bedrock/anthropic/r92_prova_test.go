package anthropic

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

// TS createAmazonBedrockAnthropic reads AWS_BEARER_TOKEN_BEDROCK once, when
// the provider is created (loadOptionalSetting,
// amazon-bedrock-anthropic-provider.ts), and picks bearer or SigV4 then.
func TestR92provaBearerTokenEnvReadAtCreation(t *testing.T) {
	var mu sync.Mutex
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		auth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKID")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	generate := func(p *BedrockAnthropicProvider) string {
		t.Helper()
		lm, err := p.LanguageModel("anthropic.claude-3-haiku-20240307-v1:0")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		return auth
	}

	p := New(Config{Region: "us-east-1", BaseURL: srv.URL})
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "late-token")
	if got := generate(p); !strings.HasPrefix(got, "AWS4-HMAC-SHA256") {
		t.Fatalf("Authorization = %q, want SigV4 (token set after creation)", got)
	}
	p = New(Config{Region: "us-east-1", BaseURL: srv.URL})
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	if got := generate(p); got != "Bearer late-token" {
		t.Fatalf("Authorization = %q, want the token read at creation", got)
	}
}
