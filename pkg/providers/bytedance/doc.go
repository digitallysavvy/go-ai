// Package bytedance is the provider for ByteDance models on the Volcengine Ark
// platform: Seedance video generation and image generation.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateVideo:
//
//	p, err := bytedance.New(bytedance.Config{
//		APIKey: os.Getenv("ARK_API_KEY"),
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	model, err := p.VideoModel("dreamina-seedance-2-0-260128")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Video generation is asynchronous. The video model submits a task and polls
// until it finishes.
//
// Guide: https://goaisdk.com/docs/providers/bytedance.
package bytedance
