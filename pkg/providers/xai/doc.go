// Package xai is the provider for xAI Grok models. Language models use the
// Responses API and support provider tools such as web search. The package
// also covers image and video generation, speech and transcription.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := xai.New(xai.Config{
//		APIKey: os.Getenv("XAI_API_KEY"),
//	})
//	model, err := p.LanguageModel(xai.ModelGrok4)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/xai.
package xai
