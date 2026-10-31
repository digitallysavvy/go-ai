package azure

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestTSCases replays the TS azure-openai-provider tests that feed recorded
// wire payloads through a model and snapshot the parts.
func TestTSCases(t *testing.T) {
	all := tsfixture.LoadCases(t, "testdata/tscases/provider.json.gz")
	for _, group := range []string{"responses", "chat", "deepseek", "completion"} {
		group := group
		var cases []tsfixture.Case
		for _, c := range all {
			if c.TopDescribe == group {
				cases = append(cases, c)
			}
		}
		t.Run(group, func(t *testing.T) {
			tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
				if id == "" {
					id = "test-deployment"
				}
				p, err := New(Config{ResourceName: "test-resource", APIKey: "k", BaseURL: base})
				if err != nil {
					t.Fatal(err)
				}
				var m provider.LanguageModel
				switch group {
				case "chat":
					m, err = p.ChatModel(id)
				case "deepseek":
					m, err = p.DeepSeekModel(id)
				case "completion":
					m, err = p.CompletionModel(id)
				default:
					m, err = p.ResponsesModel(id)
				}
				if err != nil {
					t.Fatal(err)
				}
				return m
			}, map[string]string{
				"should handle file_citation annotations without optional fields in streaming": "TS leaves responseId unset without response.created; Go falls back to response.completed id",
			})
		})
	}
}
