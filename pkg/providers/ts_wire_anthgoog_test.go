package providers_test

import (
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture/anthropictools"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
)

// Wire suites for the Anthropic / Google / Bedrock families live in this file
// so the shared registry stays untouched.
func init() {
	wireSuites["anthropic"] = tsfixture.WireSuite{
		BatchPrefix:     "/v1",
		LanguageResults: true,
		Skip: map[string]string{
			"~should not strip temperature":                                                "the TS test passes topK 0.1; Go TopK is an int",
			"AnthropicLanguageModel > doGenerate > should send the model id and settings":  "the TS test passes topK 0.1; Go TopK is an int",
			"~when transformRequestBody is provided":                                       "the transform is code in the TS test; Go has Config.TransformRequestBody",
			"~starts a batch from prepared requests and combines batch and inferred betas": "provider-level headers are not captured in the batch recording config",
			"~forwards operation headers and the abort signal while retrieving results":    "provider-level headers are not captured in the batch recording config",
		},
		Provider: func(b string, m tsfixture.WireModel) (provider.Provider, error) {
			cfg := anthropic.Config{APIKey: wireAPIKey(m), BaseURL: b, Headers: wireHeaders(m)}
			if name, _, _ := strings.Cut(m.Provider, "."); name != "" && name != "anthropic" {
				cfg.Name = name
			}
			return anthropic.New(cfg), nil
		},
		ProviderTool: func(id, name string, args map[string]interface{}) (types.Tool, bool) {
			t, ok := anthropictools.ProviderTool(id, args)
			if ok {
				if tsfixture.ToolNameAlias == nil {
					tsfixture.ToolNameAlias = map[string]string{}
				}
				if tsfixture.PromptToolNameAlias == nil {
					tsfixture.PromptToolNameAlias = map[string]string{}
				}
				tsfixture.ToolNameAlias[id], tsfixture.PromptToolNameAlias[name] = name, id
			}
			return t, ok
		},
	}
	wireSuites["google"] = tsfixture.WireSuite{
		SkipModel: func(m tsfixture.WireModel) string {
			if strings.HasPrefix(m.Provider, "google.vertex") {
				return "the shared Google model with Vertex settings is replayed through googlevertex"
			}
			return ""
		},
		BatchPrefix:     "/v1beta",
		LanguageResults: true,
		Skip: map[string]string{
			"~starts a batch with inline requests when the creation body is under 20 MB": "the TS test mocks generateId (display name) and sets provider-level headers the recording does not carry",
			"~async headers handling":                                                 "the TS test configures headers as (async) functions, which the recording does not carry",
			"~GoogleBatch > starts an inline image generation batch":                  "the TS test mocks generateId (batch display name)",
			"~GoogleBatch > omits webhook configuration for inline input":             "the TS test mocks generateId (batch display name)",
			"~GoogleBatch > surfaces Google HTTP errors from starting a batch":        "the TS test mocks generateId (batch display name)",
			"~tool-call + tool-result message pair through the request body (step 2)": "tool result JSON arrives as a Go map, which has no key order",
		},
		Provider: func(b string, m tsfixture.WireModel) (provider.Provider, error) {
			return google.New(google.Config{APIKey: wireAPIKey(m), BaseURL: b, Headers: wireHeaders(m)}), nil
		},
		Language: func(p provider.Provider, cls, id string) (provider.LanguageModel, error) {
			if strings.Contains(cls, "Interactions") {
				if strings.HasPrefix(id, "deep-research") { // TS passes { agent: id }
					return p.(*google.Provider).InteractionsAgent(id)
				}
				return p.(*google.Provider).Interactions(id)
			}
			return p.LanguageModel(id)
		},
	}
}
