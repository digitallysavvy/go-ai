// Package prodia is the provider for Prodia, a fast inference platform for
// image and video generation (FLUX, Stable Diffusion and Wan models). Model
// IDs are Prodia job types such as "inference.flux-fast.schnell.txt2img.v2".
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateImage:
//
//	p := prodia.New(prodia.Config{
//		APIKey: os.Getenv("PRODIA_TOKEN"),
//	})
//	model, err := p.ImageModel("inference.flux-fast.schnell.txt2img.v2")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/prodia.
package prodia
