// Package huggingface is the provider for Hugging Face. It talks to the
// Hugging Face Router through the Responses API
// (https://router.huggingface.co/v1/responses), so one token reaches many
// hosted models. The Responses API has no embeddings or image generation, so
// this provider offers language models only.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := huggingface.New(huggingface.Config{
//		APIKey: os.Getenv("HUGGINGFACE_API_KEY"),
//	})
//	model, err := p.LanguageModel("deepseek-ai/DeepSeek-V3-0324")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/huggingface.
//
// Provenance: this package mirrors @ai-sdk/huggingface at ai@7.0.118.
package huggingface
