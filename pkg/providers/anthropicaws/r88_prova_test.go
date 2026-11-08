package anthropicaws

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TS 6b101ac076 anthropic-aws-provider.test.ts: anthropic-aws supports
// message threads (create / continue go through with the beta).
func TestR88provaAnthropicAwsThreads(t *testing.T) {
	for _, thread := range []map[string]interface{}{{"type": "create"}, {"type": "continue", "previousMessageId": "msg_1"}} {
		var body map[string]interface{}
		var beta string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			beta = r.Header.Get("anthropic-beta")
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
		}))
		p, err := New(Config{BaseURL: srv.URL + "/v1", Region: "us-east-1", WorkspaceID: "ws", APIKey: "k"})
		if err != nil {
			t.Fatal(err)
		}
		m, _ := p.LanguageModel("claude-sonnet-4-6")
		_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
			ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thread": thread}}})
		srv.Close()
		if err != nil || body["thread"] == nil || !strings.HasPrefix(beta, "message-threads-2026-08-12") {
			t.Fatalf("%v: err %v thread %v beta %q", thread, err, body["thread"], beta)
		}
	}
}
