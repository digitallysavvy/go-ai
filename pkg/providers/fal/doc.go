// Package fal is the provider for fal, which hosts fast image, video, speech
// and transcription models. Model IDs are fal endpoint names such as "fal-
// ai/flux-pro".
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateImage:
//
//	p := fal.New(fal.Config{
//		APIKey: os.Getenv("FAL_API_KEY"),
//	})
//	model, err := p.ImageModel("fal-ai/flux-pro")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/fal.
package fal
