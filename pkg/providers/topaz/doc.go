// Package topaz is the provider for Topaz Labs. Its models enhance media you
// supply, for example by upscaling, denoising and sharpening images and video.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateImage:
//
//	p := topaz.New(topaz.Config{
//		APIKey: os.Getenv("TOPAZ_API_KEY"),
//	})
//	model, err := p.ImageModel("wonder-3.5")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/topaz.
//
// Provenance: this package mirrors @ai-sdk/topaz from the Vercel AI SDK for
// TypeScript.
package topaz
