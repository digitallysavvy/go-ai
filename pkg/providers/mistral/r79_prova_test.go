package mistral

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// TestR79prova_TSCasesPassHeaders replays both "should pass headers" cases of
// mistral-chat-language-model.test.ts (generate and stream) with the provider
// the TS test builds: createMistral({apiKey: 'test-api-key', headers:
// {'Custom-Provider-Header': ...}}). The user-agent assertion checks the Go
// SDK version in place of the TS test's mocked VERSION '0.0.0-test'. The
// generate case asserts over `const requestHeaders =
// server.calls[0].requestHeaders`, an alias the extractor did not record; it
// is restored here.
func TestR79prova_TSCasesPassHeaders(t *testing.T) {
	all := tsfixture.LoadCases(t, "testdata/tscases/chat_asserts.json.gz")
	var cases []tsfixture.Case
	evaluated := 0
	for _, c := range all {
		if c.Test != "should pass headers" {
			continue
		}
		aliases := map[string]string{"requestHeaders": "server.calls[0].requestHeaders"}
		for k, v := range c.Aliases {
			aliases[k] = v
		}
		c.Aliases = aliases
		for i, a := range c.Asserts {
			if s, ok := a.Arg.(string); ok {
				c.Asserts[i].Arg = strings.ReplaceAll(s, "0.0.0-test", version.Version)
			}
			evaluated++
		}
		cases = append(cases, c)
	}
	if len(cases) != 2 || evaluated != 4 {
		t.Fatalf("want the generate and stream cases with 2 assertions each, got %d cases, %d assertions", len(cases), evaluated)
	}
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
