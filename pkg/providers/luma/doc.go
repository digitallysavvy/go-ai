// Package luma is the provider for Luma AI image generation (Photon). Image
// generation runs asynchronously: the model submits a job and polls until the
// image is ready. It accepts reference images for guided generation.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateImage:
//
//	p := luma.New(luma.Config{
//		APIKey: os.Getenv("LUMA_API_KEY"),
//	})
//	model, err := p.ImageModel("photon-1")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/luma.
//
// Provenance: this package mirrors @ai-sdk/luma from the Vercel AI SDK for
// TypeScript.
package luma
