// Package stability is the provider for Stability AI text-to-image models.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateImage:
//
//	p := stability.New(stability.Config{
//		APIKey: os.Getenv("STABILITY_API_KEY"),
//	})
//	model, err := p.ImageModel("stable-diffusion-xl-1024-v1-0")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/stability.
package stability
