// Package anthropic is the provider for Anthropic Claude models through the
// Messages API. It supports tool use, extended thinking, prompt caching, the
// provider-executed tools in the tools subpackage, and the Files and Skills
// APIs.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := anthropic.New(anthropic.Config{
//		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
//	})
//	model, err := p.LanguageModel(anthropic.ClaudeSonnet5_5)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Use LanguageModelWithOptions to set model-level options such as beta
// headers.
//
// Guide: https://goaisdk.com/docs/providers/anthropic.
package anthropic
