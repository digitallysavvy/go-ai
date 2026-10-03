// Package alibaba is the provider for Alibaba Cloud Model Studio: the Qwen
// chat models (with context caching and reasoning), Wan video generation, and
// embeddings.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := alibaba.New(alibaba.Config{
//		APIKey: os.Getenv("ALIBABA_API_KEY"),
//	})
//	model, err := p.LanguageModel(alibaba.ModelQwen3Max)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/alibaba.
package alibaba
