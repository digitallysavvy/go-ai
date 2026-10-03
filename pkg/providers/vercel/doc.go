// Package vercel is the provider for the OpenAI-compatible chat endpoint of
// Vercel AI. It is a thin wrapper around package openai. For multi-provider
// routing, use package gateway instead.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := vercel.New(vercel.Config{
//		APIKey: os.Getenv("VERCEL_API_KEY"),
//	})
//	model, err := p.LanguageModel("your-model-id")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/vercel.
package vercel
