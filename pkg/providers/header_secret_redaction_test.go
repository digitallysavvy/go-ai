package providers_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropicaws"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// TestSerializeModel_RealProviderAuthHeadersNeverLeak is the permanent
// regression test for R1-2 (bug-review/R1.md): SerializableConfig used to
// preserve every entry of a Config.Headers map verbatim, including
// credential-bearing headers such as Anthropic's "x-api-key", Azure's
// "api-key" and Google's "x-goog-api-key". Each case below builds a real
// provider (its own Config/New/LanguageModel/Serialize -- not a synthetic
// struct) carrying a distinctive secret in the exact header that provider's
// own request-signing code uses, then asserts the secret cannot be found
// anywhere in the JSON bytes produced by marshaling SerializeModel's result.
func TestSerializeModel_RealProviderAuthHeadersNeverLeak(t *testing.T) {
	const secret = "sk-R1-2-regression-secret-do-not-leak"

	cases := []struct {
		name   string
		header string
		model  func() (provider.LanguageModel, error)
	}{
		{
			name:   "anthropic x-api-key",
			header: "x-api-key",
			model: func() (provider.LanguageModel, error) {
				p := anthropic.New(anthropic.Config{
					Headers: map[string]string{"x-api-key": secret},
				})
				return p.LanguageModel("claude-sonnet-4-20250514")
			},
		},
		{
			name:   "azure api-key",
			header: "api-key",
			model: func() (provider.LanguageModel, error) {
				p, err := azure.New(azure.Config{
					ResourceName: "test-resource",
					Headers:      map[string]string{"api-key": secret},
				})
				if err != nil {
					return nil, err
				}
				return p.LanguageModel("gpt-4o")
			},
		},
		{
			name:   "google x-goog-api-key",
			header: "x-goog-api-key",
			model: func() (provider.LanguageModel, error) {
				p := google.New(google.Config{
					Headers: map[string]string{"x-goog-api-key": secret},
				})
				return p.LanguageModel("gemini-2.0-flash")
			},
		},
		{
			name:   "google-vertex x-goog-api-key",
			header: "x-goog-api-key",
			model: func() (provider.LanguageModel, error) {
				p, err := googlevertex.New(googlevertex.Config{
					Project:     "test-project",
					Location:    "us-central1",
					AccessToken: "unrelated-access-token",
					Headers:     map[string]string{"x-goog-api-key": secret},
				})
				if err != nil {
					return nil, err
				}
				return p.LanguageModel("gemini-2.0-flash")
			},
		},
		{
			name:   "openai Authorization",
			header: "Authorization",
			model: func() (provider.LanguageModel, error) {
				p := openai.New(openai.Config{
					Headers: map[string]string{"Authorization": "Bearer " + secret},
				})
				return p.LanguageModel("gpt-4o")
			},
		},
		{
			// anthropicaws.New bakes its API-key auth directly into the
			// inner anthropic.Config.Headers map (see
			// pkg/providers/anthropicaws/provider.go) -- this is the exact
			// real-world chain the R1 bug report reproduced: the secret
			// never touches a dedicated APIKey struct field at all, only
			// Config.Headers["x-api-key"].
			name:   "anthropic-aws baked-in x-api-key",
			header: "x-api-key",
			model: func() (provider.LanguageModel, error) {
				p, err := anthropicaws.New(anthropicaws.Config{
					Region:      "us-east-1",
					WorkspaceID: "ws-1",
					APIKey:      secret,
				})
				if err != nil {
					return nil, err
				}
				return p.LanguageModel("claude-sonnet-4-20250514")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model, err := tc.model()
			if err != nil {
				t.Fatalf("construct model: %v", err)
			}
			serialized, err := provider.SerializeModel(model)
			if err != nil {
				t.Fatalf("SerializeModel: %v", err)
			}
			data, err := json.Marshal(serialized)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if strings.Contains(string(data), secret) {
				t.Fatalf("secret leaked via header %q into serialized JSON: %s", tc.header, data)
			}
			// Confirm this case actually exercised the redaction path
			// (the header key itself may legitimately still be present
			// with its value stripped/omitted -- only the secret VALUE
			// must never appear).
			if headers, ok := serialized.Config["Headers"].(map[string]interface{}); ok {
				if v, present := headers[tc.header]; present {
					t.Fatalf("header %q present with unredacted value: %v", tc.header, v)
				}
			}
		})
	}
}
