package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS doGenerate returns response.body = rawValue (anthropic-language-model.ts
// 1597 / 2159): the parsed JSON with every key the API sent. Go returned the
// typed anthropicResponse, so undeclared keys were missing
// (cr-r89-provg-to-prova).
func TestR89integAnthropicRawResponseIsRawBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1},"zzExtra":1}`)
	}))
	defer srv.Close()
	model, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("claude-sonnet-4-5")
	if err != nil {
		t.Fatal(err)
	}
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := res.RawResponse.(map[string]interface{})
	if !ok {
		t.Fatalf("RawResponse = %T, want the raw JSON object", res.RawResponse)
	}
	if raw["zzExtra"] != float64(1) || raw["id"] != "m" {
		t.Fatalf("RawResponse = %v", raw)
	}
}
