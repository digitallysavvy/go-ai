package anthropic

import "github.com/digitallysavvy/go-ai/pkg/provider"

// r80provaCustomProviderName is the provider name of the TS test's
// createCustomModel(name): createAnthropic({name})('claude-3-haiku-20240307').
// The extractor records the name as the case's model id.
const r80provaCustomProviderName = "my-custom-anthropic"

// r80provaAnthropicCaseModel builds the model a language case runs against:
// the default provider, or for the createCustomModel cases a provider named
// my-custom-anthropic (Config.Name, TS createAnthropic name).
func r80provaAnthropicCaseModel(base, id string) (provider.LanguageModel, error) {
	if id == r80provaCustomProviderName {
		return New(Config{APIKey: "test-api-key", BaseURL: base, Name: r80provaCustomProviderName}).LanguageModel("claude-3-haiku-20240307")
	}
	return New(Config{APIKey: "test-api-key", BaseURL: base}).LanguageModel(id)
}
