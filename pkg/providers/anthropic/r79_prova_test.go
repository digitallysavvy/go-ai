package anthropic

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// TestR79prova_TSCasesProviderHeaders replays the anthropic-language-model
// cases that build their own provider with createAnthropic({apiKey:
// 'test-api-key', headers}) and snapshot the request headers: "should pass
// headers" (generate and stream) and "should merge custom anthropic-beta
// headers without legacy fine-grained-tool-streaming beta".
func TestR79prova_TSCasesProviderHeaders(t *testing.T) {
	headers := map[string]map[string]string{
		"should pass headers": {"Custom-Provider-Header": "provider-header-value"},
		"should merge custom anthropic-beta headers without legacy fine-grained-tool-streaming beta": {"anthropic-beta": "CONFIG-beta1,config-beta2"},
	}
	all := tsfixture.LoadCases(t, "testdata/tscases/language.json.gz")
	byTest := map[string][]tsfixture.Case{}
	for _, c := range all {
		if _, ok := headers[c.Test]; ok {
			byTest[c.Test] = append(byTest[c.Test], c)
		}
	}
	if len(byTest["should pass headers"]) != 2 || len(byTest) != 2 {
		t.Fatalf("missing cases: %d tests", len(byTest))
	}
	tsfixture.EvalPathSnapshots(t)
	for name, cases := range byTest {
		h := headers[name]
		tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
			m, err := New(Config{APIKey: "test-api-key", BaseURL: base, Headers: h}).LanguageModel(id)
			if err != nil {
				t.Fatal(err)
			}
			return m
		}, nil)
	}
}

// r79provaGoVersion rewrites the TS test package VERSION ('0.0.0-test',
// mocked in the TS tests) in plain assertion arguments to the Go SDK
// version, so the user-agent assertions check the Go user agent
// (ai-sdk-<provider>/<version>).
func r79provaGoVersion(cases []tsfixture.Case) []tsfixture.Case {
	for ci := range cases {
		for i, a := range cases[ci].Asserts {
			if s, ok := a.Arg.(string); ok {
				cases[ci].Asserts[i].Arg = strings.ReplaceAll(s, "0.0.0-test", version.Version)
			}
		}
	}
	return cases
}
