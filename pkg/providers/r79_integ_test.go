package providers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	vertexxai "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// r79integCompatModels builds every OpenAI-compatible generate path whose
// TS twin is @ai-sdk/openai-compatible (created: z.number().nullish(),
// response metadata created || undefined).
func r79integCompatModels(t *testing.T, base string) map[string]provider.LanguageModel {
	t.Helper()
	mk := func(m provider.LanguageModel, err error) provider.LanguageModel {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	vx, err := vertexxai.New(vertexxai.Config{Project: "p", Location: "us-central1", AccessToken: "t", BaseURL: base})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]provider.LanguageModel{
		"gmicloud":  mk(gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")),
		"together":  mk(together.New(together.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")),
		"fireworks": mk(fireworks.New(fireworks.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")),
		"zai":       mk(zai.New(zai.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")),
		"ollama":    mk(ollama.New(ollama.Config{BaseURL: base}).LanguageModel("m")),
		"vertexxai": mk(vx.LanguageModel("m")),
	}
}

func r79integServe(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

const r79integChatBody = `{"id":"c","object":"chat.completion","created":%s,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`

// provb CR r79 items 1-2 and the prova-forwarded googlevertex/xai item: a
// fractional created decodes (new Date(created * 1000)), and created 0 is no
// timestamp (created || undefined).
func TestR79integCompatCreated(t *testing.T) {
	for _, tc := range []struct {
		created string
		want    time.Time
	}{
		{"1700000000.25", time.UnixMilli(1700000000250)},
		{"0", time.Time{}},
		{"1700000000", time.Unix(1700000000, 0)},
	} {
		srv := r79integServe(fmt.Sprintf(r79integChatBody, tc.created))
		for name, m := range r79integCompatModels(t, srv.URL) {
			res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
			if err != nil {
				t.Fatalf("%s created=%s: %v", name, tc.created, err)
			}
			var got time.Time
			if res.ResponseMetadata != nil {
				got = res.ResponseMetadata.Timestamp
			}
			if !got.Equal(tc.want) {
				t.Errorf("%s created=%s: timestamp %v, want %v", name, tc.created, got, tc.want)
			}
		}
		srv.Close()
	}
}

// prova CR r79 proposal 6: a fractional usage count (TS z.number()) no
// longer fails a non-stream generate decode into an int field.
func TestR79integFractionalUsageGenerate(t *testing.T) {
	srv := r79integServe(`{"id":"c","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1.5,"completion_tokens":2,"total_tokens":3.5}}`)
	defer srv.Close()
	m, err := groq.New(groq.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hi" || res.Usage.InputTokens == nil || *res.Usage.InputTokens != 1 {
		t.Fatalf("got text %q usage %+v", res.Text, res.Usage)
	}
}
