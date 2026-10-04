// Package deepinfra is the provider for DeepInfra, which hosts open-weight
// chat and image models on serverless GPUs.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := deepinfra.New(deepinfra.Config{
//		APIKey: os.Getenv("DEEPINFRA_API_KEY"),
//	})
//	model, err := p.LanguageModel("meta-llama/Llama-3.3-70B-Instruct")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/deepinfra.
package deepinfra
