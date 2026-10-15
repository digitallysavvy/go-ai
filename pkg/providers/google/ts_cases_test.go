package google

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

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
		m, err := New(Config{APIKey: "k", BaseURL: base}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, nil)
}
