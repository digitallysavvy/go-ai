package perplexity

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS perplexity-decision-model.ts posts {model, state, questions}, each
// question as {type, instructions, criteria}, with JSON.stringify.
func TestR98provgDecisionRequestKeyOrder(t *testing.T) {
	fixture, err := os.ReadFile("testdata/decisions.json")
	if err != nil {
		t.Fatal(err)
	}
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL + "/"}).DecisionModel("pplx-decider-latest")
	if err != nil {
		t.Fatal(err)
	}
	opts := provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{provider.DecisionJSONPart(map[string]interface{}{"review": "ok"})},
		Questions: map[string]provider.DecisionQuestion{
			"defect": {Type: "boolean", Instructions: "Defect?", Criteria: "a defect"},
		},
	}
	_, _ = m.DoDecide(context.Background(), opts) // the fixture's answers do not matter here
	want := `{"model":"pplx-decider-latest","state":{"review":"ok"},"questions":{"defect":{"type":"noul","instructions":"Defect?","criteria":"a defect"}}}`
	if body != want {
		t.Fatalf("body =\n%s\nwant\n%s", body, want)
	}
}
