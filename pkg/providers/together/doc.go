// Package together is the provider for Together AI, which hosts open-weight
// chat, embedding, image and reranking models.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := together.New(together.Config{
//		APIKey: os.Getenv("TOGETHER_API_KEY"),
//	})
//	model, err := p.LanguageModel("meta-llama/Llama-3.3-70B-Instruct-Turbo")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/together.
package together
