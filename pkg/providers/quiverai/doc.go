// Package quiverai is the provider for QuiverAI, which generates SVG graphics
// from a prompt and converts raster images to SVG.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateImage:
//
//	p := quiverai.New(quiverai.Config{
//		APIKey: os.Getenv("QUIVERAI_API_KEY"),
//	})
//	model, err := p.ImageModel("arrow-1")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/quiverai.
package quiverai
