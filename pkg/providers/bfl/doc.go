// Package bfl is the provider for Black Forest Labs, the maker of the FLUX
// image models. It generates and edits images through the shared image model
// interface, and also offers a video model.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateImage:
//
//	p := bfl.New(bfl.Config{
//		APIKey: os.Getenv("BFL_API_KEY"),
//	})
//	model, err := p.ImageModel("flux-pro-1.1")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/bfl.
package bfl
