package anthropicaws

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

// prova r90 open item: on a redirect chain API -> foreign -> API, fetch drops
// the signed Authorization on the foreign hop and never adds it back, so the
// SigV4 transport must not re-sign the hop back to the API origin. An
// x-api-key header is a header fetch keeps, so API-key mode still sends it
// to the API origin.
func TestR90integSigV4NotResignedAfterForeignHop(t *testing.T) {
	for _, mode := range []string{"sigv4", "apikey"} {
		var mu sync.Mutex
		var back http.Header
		var apiURL string
		foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			http.Redirect(w, r, apiURL+"/v1/back", http.StatusTemporaryRedirect)
		}))
		foreignURL := strings.Replace(foreign.URL, "127.0.0.1", "localhost", 1)
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			if r.URL.Path == "/v1/back" {
				mu.Lock()
				back = r.Header.Clone()
				mu.Unlock()
				w.WriteHeader(http.StatusTeapot)
				return
			}
			http.Redirect(w, r, foreignURL+"/bounce", http.StatusTemporaryRedirect)
		}))
		apiURL = api.URL
		cfg := Config{BaseURL: api.URL + "/v1", Region: "us-east-1", WorkspaceID: "ws"}
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
		mu.Lock()
		got := back
		mu.Unlock()
		foreign.Close()
		api.Close()
		if got == nil {
			t.Fatalf("%s: the redirect chain did not come back to the API origin", mode)
		}
		if v := got.Get("Authorization"); v != "" {
			t.Errorf("%s: hop back to the API origin was re-signed: Authorization %q", mode, v)
		}
		if mode == "apikey" && got.Get("X-Api-Key") != "tok" {
			t.Errorf("apikey: hop back to the API origin lost x-api-key (fetch keeps it): %v", got)
		}
	}
}
