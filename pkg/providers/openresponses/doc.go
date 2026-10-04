// Package openresponses is the provider for servers that implement the Open
// Responses API, such as LM Studio, Ollama and LocalAI. Point BaseURL at the
// server. The package also supports Open Responses extensions and custom
// tools.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := openresponses.New(openresponses.Config{
//		BaseURL: "http://localhost:1234/v1",
//		Name:    "lmstudio",
//	})
//	model, err := p.LanguageModel("my-local-model")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/openresponses.
package openresponses
