package googlevertex

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestTSCasesVertexLanguage replays the cases of the TS google language model
// test that exercise Vertex-only behavior with a `google.vertex.chat` model. The case file lives with the google provider.
func TestTSCasesVertexLanguage(t *testing.T) {
	tsfixture.DropResponseTimestamp, tsfixture.DropResponseModelID = true, true
	tsfixture.MetadataKeyAlias = map[string]string{"googleVertex": "vertex"}
	defer func() {
		tsfixture.DropResponseTimestamp, tsfixture.DropResponseModelID, tsfixture.MetadataKeyAlias = false, false, nil
	}()
	all := tsfixture.LoadCases(t, "../google/testdata/tscases/language.json.gz")
	var cases []tsfixture.Case
	for _, c := range all {
		// the Gemini API variants of these tests run in the google package
		if r80provaVertexOnlyCase(c.Test) {
			cases = append(cases, c)
		}
	}
	if len(cases) == 0 {
		t.Fatal("no Vertex cases found")
	}
	tsfixture.EvalPathSnapshots(t)
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "gemini-pro"
		}
		p, err := New(Config{Project: "p", Location: "l", AccessToken: "test-token", BaseURL: base})
		if err != nil {
			t.Fatal(err)
		}
		m, err := p.LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, nil)
}

// r80provaVertexOnlyCase reports whether a google language case builds a
// `google.vertex.chat` model (the google package skips these).
func r80provaVertexOnlyCase(name string) bool {
	return strings.Contains(name, "for Vertex") || strings.HasSuffix(name, "on Vertex") || strings.Contains(name, "from Vertex requests") ||
		strings.Contains(name, `when provider includes "vertex"`)
}
