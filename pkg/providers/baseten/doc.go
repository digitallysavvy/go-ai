// Package baseten is the provider for Baseten Model APIs and your own Baseten
// deployments. Baseten exposes an OpenAI-compatible endpoint, and this package
// builds on package openai.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := baseten.New(baseten.Config{
//		APIKey: os.Getenv("BASETEN_API_KEY"),
//	})
//	model, err := p.LanguageModel("deepseek-ai/DeepSeek-V3.1")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/baseten.
package baseten
