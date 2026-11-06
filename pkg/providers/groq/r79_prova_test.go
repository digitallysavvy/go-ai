package groq

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// TestR79prova_TSCasesPassHeaders replays both "should pass headers" cases of
// groq-chat-language-model.test.ts (generate and stream) with the provider
// the TS test builds: createGroq({apiKey: 'test-api-key', headers:
// {'Custom-Provider-Header': ...}}). The request-header snapshot is compared
// (EvalPathSnapshots) and the user-agent assertion checks the Go SDK version
// in place of the TS test's mocked VERSION '0.0.0-test'.
func TestR79prova_TSCasesPassHeaders(t *testing.T) {
	all := tsfixture.LoadCases(t, "testdata/tscases/chat_asserts.json.gz")
	var cases []tsfixture.Case
	for _, c := range all {
		if c.Test != "should pass headers" {
			continue
		}
		for i, a := range c.Asserts {
			if s, ok := a.Arg.(string); ok {
				c.Asserts[i].Arg = strings.ReplaceAll(s, "0.0.0-test", version.Version)
			}
		}
		cases = append(cases, c)
	}
	if len(cases) != 2 {
		t.Fatalf("want the generate and stream cases, got %d", len(cases))
	}
	tsfixture.EvalPathSnapshots(t)
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		m, err := New(Config{
			APIKey:  "test-api-key",
			BaseURL: base,
			Headers: map[string]string{"Custom-Provider-Header": "provider-header-value"},
		}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, nil)
}
