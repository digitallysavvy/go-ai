// Package ollama is the provider for Ollama, which runs open-weight models on
// your own machine. It supports chat with tools and embeddings, with no API
// key.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := ollama.New(ollama.Config{
//		BaseURL: "http://localhost:11434",
//	})
//	model, err := p.LanguageModel("llama3.1")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/ollama.
package ollama
