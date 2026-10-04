// Package providers is the parent of the provider packages. It contains no
// code of its own: each provider lives in a subdirectory, such as
// pkg/providers/openai, pkg/providers/anthropic and pkg/providers/google. The
// tests in this directory check behavior that every provider must share,
// for example header handling and model serialization.
//
// Import the provider you need, create it with its New function, and ask it for
// a model:
//
//	p := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
//	model, err := p.LanguageModel(openai.ModelGPT6Astra)
//
// Every provider implements provider.Provider, so you can swap one for another
// without changing the code that calls package ai.
//
// The list of providers and their setup guides: https://goaisdk.com/docs/providers.
package providers
