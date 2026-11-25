package google

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const (
	r87dgooglefamBackgroundTest = "omits background by default (no auto-injection on agent calls)"
	r87dgooglefamJSONResultTest = "serializes a json tool result to a stringified payload"
)

// r87dgooglefamPorted are the TS plain assertions the replay cannot evaluate,
// each reproduced by the named Go test with the same input and expected value.
var r87dgooglefamPorted = map[string]string{
	// the test's second model call (the replay runs the first one)
	tsfixture.PortedKey(r87dgooglefamBackgroundTest, "modelBody.background", "toBeUndefined"): "TestR87dgooglefamBackgroundOmittedOnModelCall",
}

// r87dgooglefamNotApplicable are TS plain assertions that cannot apply to Go.
var r87dgooglefamNotApplicable = map[string]string{
	tsfixture.PortedKey(r87dgooglefamJSONResultTest, "fr.result", "toBe"): "the JSON tool result value is a Go map, so JSON.stringify's insertion order " +
		"({\"temperature\":72,\"condition\":\"sunny\"}) comes out in Go's sorted key order; the values match " +
		"(known difference \"Tool output JSON key order in replayed prompts\")",
}

func init() {
	tsfixture.RequireEvaluated()
	tsfixture.RegisterPorted(r87dgooglefamPorted)
	tsfixture.RegisterNotApplicable(r87dgooglefamNotApplicable)
}

func TestMain(m *testing.M) {
	os.Exit(tsfixture.PortedMain(m, r87dgooglefamPorted, r87dgooglefamNotApplicable))
}

// TestR87dgooglefamBackgroundOmittedOnModelCall ports the second half of TS
// interactions/google-interactions-language-model.test.ts "omits background
// by default (no auto-injection on agent calls)": after an agent call, a call
// on the model-id model (`provider.interactions('gemini-2.5-flash')`) with
// TEST_PROMPT sends no `background` (server.calls[1]).
func TestR87dgooglefamBackgroundOmittedOnModelCall(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/interactions.json.gz")
	c, a, ok := tsfixture.FindAssertion(cases, r87dgooglefamBackgroundTest, "modelBody.background", "toBeUndefined")
	if !ok || c.Response == nil || c.Response.Raw == "" {
		t.Fatal("case or its fixture response not found")
	}
	var mu sync.Mutex
	var bodies []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]interface{}
		_ = json.Unmarshal(b, &body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, c.Response.Raw)
	}))
	defer srv.Close()
	p := New(Config{APIKey: "test-api-key", BaseURL: srv.URL})
	agent, err := p.InteractionsAgent("deep-research-pro-preview-12-2025")
	if err != nil {
		t.Fatal(err)
	}
	model, err := p.Interactions("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	prompt := types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello, how are you?"}}}}}
	for _, m := range []provider.LanguageModel{agent, model} {
		if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: prompt}); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("got %d requests, want 2", len(bodies))
	}
	if _, has := bodies[0]["background"]; has {
		t.Errorf("agent body has background: %v", bodies[0]["background"])
	}
	if bodies[1]["model"] != "gemini-2.5-flash" {
		t.Fatalf("second request model = %v", bodies[1]["model"])
	}
	if v, has := bodies[1]["background"]; has {
		t.Errorf("model body has background: %v (TS: undefined)", v)
	}
	if msg, ok := tsfixture.MatchAssertion(a, bodies[1]["background"]); !ok || msg != "" {
		t.Errorf("modelBody.background: %s (evaluated %v)", msg, ok)
	}
}
