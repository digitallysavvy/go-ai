package perplexity

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// created_at is a zod number: a fractional second is valid and the empty
// thought signature (not null) is kept in the tool-call metadata.
func TestR44_FractionalCreatedAtAndEmptyThoughtSignature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r1","created_at":1700000000.5,"model":"m","object":"response","status":"completed","output":[{"type":"function_call","id":"i1","call_id":"c1","name":"f","arguments":"{}","thought_signature":""}]}`))
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{BaseURL: srv.URL, APIKey: "k"}), "low")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.ResponseMetadata == nil || res.ResponseMetadata.Timestamp.UnixMilli() != 1700000000500 {
		t.Fatalf("timestamp = %v", res.ResponseMetadata)
	}
	var found bool
	for _, p := range res.Content {
		if tc, ok := p.(types.ToolCallContent); ok {
			found = strings.Contains(string(tc.ProviderMetadata), `"thoughtSignature":""`)
		}
	}
	if !found {
		t.Fatalf("empty thoughtSignature dropped: %#v", res.Content)
	}
}
