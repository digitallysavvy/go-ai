// Package fireworks is the provider for Fireworks AI, which serves open-weight
// chat, embedding and image models. It supports reasoning content and
// Fireworks async image models.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := fireworks.New(fireworks.Config{
//		APIKey: os.Getenv("FIREWORKS_API_KEY"),
//	})
//	model, err := p.LanguageModel("accounts/fireworks/models/llama-v3p1-70b-instruct")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/fireworks.
package fireworks
