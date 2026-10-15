package providers_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture/anthropictools"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
	vertexanthropic "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex/anthropic"
)

// Wire suites for the Anthropic / Google / Bedrock families live in this file
// so the shared registry stays untouched.
func init() {
	wireSuites["anthropic"] = tsfixture.WireSuite{
		RawSchemas:      true,
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
		ProviderTool: anthropicWireTool,
	}
	wireSuites["google"] = tsfixture.WireSuite{
		RawSchemas: true,
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
	wireSuites["amazon-bedrock"] = tsfixture.WireSuite{
		RawSchemas:      true,
		RootBase:        true,
		LanguageResults: true,
		SkipStream:      "the recording keeps the Bedrock binary event-stream frames as text; stream parsing is replayed by the case suites",
		ProviderTool:    anthropicWireTool,
		Skip: map[string]string{
			"~should handle Anthropic provider-defined tools":                             "the TS test mocks prepareAnthropicTools, and Go sends its own tool JSON schema (property descriptions) where TS sends the zod schema",
			"~should combine user-provided and tool-generated betas in body":              "the TS test mocks prepareAnthropicTools and leaks anthropicBeta into the body (accepted TS bug)",
			"~should not include anthropic-beta in HTTP headers":                          "the TS test mocks prepareAnthropicTools and leaks anthropicBeta into the body (accepted TS bug)",
			"~anthropic_beta in additionalModelRequestFields when using extended context": "the TS test mocks prepareAnthropicTools and leaks anthropicBeta into the body (accepted TS bug)",
			"~should work with partial headers":                                           "the TS test signs the request with SigV4 and a signed header; Go uses the bearer token here",
			"~Cohere models behind application inference profile ARNs":                    "modelFamily is a per-model setting in the TS test",
			"~should properly combine headers from all sources":                           "the TS test signs the request with SigV4 and a signed header; Go uses the bearer token here",
		},
		SkipModel: func(m tsfixture.WireModel) string {
			if m.Cls != "AmazonBedrockChatLanguageModel" && m.Cls != "AmazonBedrockImageModel" && m.Cls != "AmazonBedrockEmbeddingModel" && m.Cls != "AmazonBedrockRerankingModel" {
				return "replayed through the Anthropic / Mantle sub-providers"
			}
			return ""
		},
		Provider: func(b string, m tsfixture.WireModel) (provider.Provider, error) {
			h := wireHeaders(m)
			h["x-amz-auth"] = "test-auth" // the TS tests inject it through a custom fetch
			region := "us-east-1"
			if r, ok := m.Config["region"].(string); ok && r != "" {
				region = r
			}
			return bedrock.New(bedrock.Config{APIKey: "test-api-key", Region: region, BaseURL: b, Headers: h}), nil
		},
	}
	wireSuites["google-vertex"] = tsfixture.WireSuite{
		RawSchemas:      true,
		LanguageResults: true,
		ProviderTool:    anthropicWireTool,
		Skip: map[string]string{
			"~Vertex Anthropic labels >":                                     "provider-level headers are not captured in the recording config, and the labels JSON keeps the TS object key order, which a Go map cannot",
			"~tools and thinking config for":                                 "the TS test sets Authorization as a provider-level header, which the recording does not carry",
			"~predict dimensions >":                                          "the TS fixture returns two predictions for one value and TS does not check; Go rejects a count mismatch",
			"~should pass headers and user agent":                            "provider-level headers are not captured in the recording config",
			"~GoogleVertexEmbeddingModel > should pass headers correctly":    "provider-level headers are not captured in the recording config",
			"~should preserve local JSON Schema references in tool requests": "the TS test creates the provider with apiKey (express mode), which the recording does not carry",
		},
		Provider: func(b string, m tsfixture.WireModel) (provider.Provider, error) {
			u, err := url.Parse(b)
			if err != nil {
				return nil, err
			}
			// Speech-to-text and Cloud TTS use fixed Google hosts: send every
			// request to the replay server, keeping the path.
			client := &http.Client{Transport: hostRewriter{host: u.Host, scheme: u.Scheme}}
			if m.Cls == "AnthropicLanguageModel" {
				return vertexanthropic.New(vertexanthropic.Options{Project: "test-project", Location: "us-east5", BaseURL: b, HTTPClient: client,
					AuthToken: func(context.Context) (string, error) { return "test-token", nil }, Headers: wireHeaders(m)}), nil
			}
			token := "test-oauth-token"
			hdrs := wireHeaders(m)
			if auth, ok := hdrs["Authorization"]; ok { // the TS test sets the token as a provider header
				token = strings.TrimPrefix(auth, "Bearer ")
				delete(hdrs, "Authorization")
			}
			return googlevertex.New(googlevertex.Config{Project: "test-project", Location: "us-central1", AccessToken: token,
				BaseURL: b, CloudTTSBaseURL: u.Scheme + "://" + u.Host + "/v1/text:synthesize", HTTPClient: client, Headers: hdrs})
		},
	}
}

// hostRewriter sends every request to one host, keeping the path and query.
type hostRewriter struct{ host, scheme string }

func (h hostRewriter) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.URL.Host, r2.URL.Scheme, r2.Host = h.host, h.scheme, h.host
	return http.DefaultTransport.RoundTrip(r2)
}

// anthropicWireTool maps a TS Anthropic provider tool and aliases its name.
func anthropicWireTool(id, name string, args map[string]interface{}) (types.Tool, bool) {
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
}
