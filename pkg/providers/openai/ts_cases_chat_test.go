package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestTSCasesChat(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/chat.json.gz")
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "gpt-3.5-turbo"
		}
		m, err := New(Config{APIKey: "k", BaseURL: base}).ChatModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, nil)
}

func TestTSCasesResponses(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/responses.json.gz")
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "gpt-4o"
		}
		m, err := New(Config{APIKey: "k", BaseURL: base}).ResponsesModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, map[string]string{
		"should parse compaction output item from real fixture":              "Go keeps the v0.5.2 flat compaction metadata keys next to the TS providerMetadata.openai object",
		"should stream with encrypted content include reasoning-delta part": "TS fixture concatenates several responses into one stream; Go finishes at the first response.completed",
	})
}
