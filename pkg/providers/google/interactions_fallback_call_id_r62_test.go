package google

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r62 integrator (coordinator ruling): a function_call without an id takes
// its ID from the provider's ID generator, not a per-response `call_N`
// counter, so id-less calls in two steps of one run never share an ID. An id
// the API sends is kept and does not consume a generated ID.
func TestR62InteractionsIDlessFunctionCallUsesGenerator(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"id":"v1","status":"requires_action","steps":[
			{"type":"function_call","name":"lookup","arguments":{}},
			{"type":"function_call","id":"srv-1","name":"lookup","arguments":{}},
			{"type":"function_call","id":"","name":"lookup","arguments":{}}
		]}`)
	}))
	defer server.Close()

	t.Run("configured generator", func(t *testing.T) {
		n := 0
		cfg := interactionsConfigFromProvider(New(Config{APIKey: "k", BaseURL: server.URL}))
		cfg.GenerateID = func() string { n++; return fmt.Sprintf("gen-%d", n) }
		model := NewInteractionsLanguageModelWithConfig(cfg, ModelGemini25Flash)
		var got []string
		for step := 0; step < 2; step++ {
			res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range res.ToolCalls {
				got = append(got, c.ID)
			}
		}
		if fmt.Sprint(got) != "[gen-1 srv-1 gen-2 gen-3 srv-1 gen-4]" {
			t.Fatalf("tool call IDs = %v", got)
		}
	})

	t.Run("default generator", func(t *testing.T) {
		model := NewInteractionsLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelGemini25Flash)
		seen := map[string]bool{}
		for step := 0; step < 2; step++ {
			res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range res.ToolCalls {
				if c.ID == "srv-1" {
					continue
				}
				if c.ID == "" || seen[c.ID] {
					t.Fatalf("id-less call got ID %q (seen %v)", c.ID, seen)
				}
				seen[c.ID] = true
			}
		}
	})
}
