// Package google is the provider for Google Gemini models through Google AI
// Studio (the Generative Language API). It covers chat with tools, structured
// output and thinking, plus embeddings, image generation, speech,
// transcription, video generation and batches.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := google.New(google.Config{
//		APIKey: os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY"),
//	})
//	model, err := p.LanguageModel(google.ModelGemini31FlashLitePreview)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// The Gemini request and streaming code that this package shares with
// googlevertex lives in package gemini.
//
// Guide: https://goaisdk.com/docs/providers/google.
package google
