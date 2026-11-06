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
	}, map[string]string{
		// The extractor evaluates the runtime alias `signature`
		// (readThemeCall.providerMetadata.vertex.thoughtSignature) to the
		// describe-level `signature` constant of another block; Go's value is
		// the fixture's signature, as in TS. The ordering and signature
		// placement are compared part by part in TestR87provaVertexStreamOrderMatchesTS.
		"should emit no-args function calls and preserve thoughtSignature alongside streamed-args calls": "extractor alias artifact (see comment)",
	})
}

// r80provaVertexOnlyCase reports whether a google language case builds a
// `google.vertex.chat` model (the google package skips these).
func r80provaVertexOnlyCase(name string) bool {
	return strings.Contains(name, "for Vertex") || strings.HasSuffix(name, "on Vertex") || strings.Contains(name, "from Vertex requests") ||
		strings.Contains(name, `when provider includes "vertex"`) ||
		strings.Contains(name, "supported Vertex tool result URLs") || strings.HasSuffix(name, "with Vertex provider") ||
		name == "should emit no-args function calls and preserve thoughtSignature alongside streamed-args calls"
}
