package google

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const asyncHeaders = "the TS test sets async provider-level headers on the model config; the harness builds a default provider"

const multiResponse = "the TS test queues several responses; the harness serves one canned response (covered by long_decode_test.go)"

func TestTSCasesLanguage(t *testing.T) {
	tsfixture.DropResponseTimestamp, tsfixture.DropResponseModelID = true, true
	defer func() { tsfixture.DropResponseTimestamp, tsfixture.DropResponseModelID = false, false }()
	tsfixture.MetadataKeyAlias = map[string]string{"googleVertex": "google", "vertex": "google"}
	defer func() { tsfixture.MetadataKeyAlias = nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/language.json.gz")
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "gemini-pro"
		}
		m, err := New(Config{APIKey: "test-api-key", BaseURL: base}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, map[string]string{
		"should continue the stream automatically on CONTINUATION":                                                           multiResponse,
		"should finish with the last continuation token when continuing the stream fails":                                    multiResponse,
		"should resend the same request with the continuation token on CONTINUATION":                                         multiResponse,
		"should return the continuation token when the budget is used up after continuing":                                   multiResponse,
		"should pass penalty settings for Vertex Gemini 2.5 models":                                                          "Vertex-only behavior; replayed by googlevertex/ts_cases_test.go",
		"should pass imageConfig.personGeneration, imageConfig.prominentPeople and imageConfig.imageOutputOptions on Vertex": "Vertex-only behavior; replayed by googlevertex/ts_cases_test.go",
		"merges async config headers with sync request headers":                                                              asyncHeaders,
		"handles Promise-based headers":                                                                                      asyncHeaders,
		"handles async function headers from config":                                                                         asyncHeaders,
	})
}
