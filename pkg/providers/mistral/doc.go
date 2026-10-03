// Package mistral is the provider for Mistral AI. It offers chat models with
// tool use, embeddings, speech synthesis and transcription.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := mistral.New(mistral.Config{
//		APIKey: os.Getenv("MISTRAL_API_KEY"),
//	})
//	model, err := p.LanguageModel("mistral-large-latest")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/mistral.
package mistral
