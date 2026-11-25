package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Round 77 integrator survivor test (provb fix 5): a non-streaming Responses
// result with created_at 0 has the epoch timestamp (TS new Date(0)).
func TestR77Integ_GenerateCreatedAtZeroIsEpoch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","created_at":0,"status":"completed","model":"gpt-4o","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.ResponseMetadata == nil || r.ResponseMetadata.Timestamp.IsZero() || !r.ResponseMetadata.Timestamp.Equal(time.UnixMilli(0)) {
		t.Fatalf("response metadata = %+v, want the epoch timestamp", r.ResponseMetadata)
	}
}
