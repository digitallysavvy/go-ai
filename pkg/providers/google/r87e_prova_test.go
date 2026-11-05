package google

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS google-language-model.ts: providerMetadata.google.serviceTier is
// `usageMetadata?.serviceTier ?? null`, so an empty tier the API sent stays
// "" (generate and stream).
func TestR87eprovaServiceTierEmptyStringReported(t *testing.T) {
	const usage = `"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"serviceTier":""}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],` + usage + `}`
		if r.URL.Query().Get("alt") == "sse" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()
	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelGemini25Flash)
	tier := func(meta interface{}) string {
		b, _ := json.Marshal(meta)
		var m map[string]map[string]json.RawMessage
		_ = json.Unmarshal(b, &m)
		return string(m["google"]["serviceTier"])
	}

	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := tier(res.ProviderMetadata); got != `""` {
		t.Errorf("generate serviceTier = %s, want \"\"", got)
	}

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	got := ""
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeFinish {
			got = tier(chunk.ProviderMetadata)
		}
	}
	if got != `""` {
		t.Errorf("stream serviceTier = %s, want \"\"", got)
	}
}
