// Package klingai is the provider for KlingAI video generation: text-to-video,
// image-to-video, start and end frame control, and motion control. Each
// request is signed with a short-lived JWT built from your access key and
// secret key.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateVideo:
//
//	p, err := klingai.New(klingai.Config{
//		AccessKey: os.Getenv("KLINGAI_ACCESS_KEY"),
//		SecretKey: os.Getenv("KLINGAI_SECRET_KEY"),
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	model, err := p.VideoModel("kling-v2.6-t2v")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Video generation is asynchronous. The video model submits a task and polls
// until it finishes.
//
// Guide: https://goaisdk.com/docs/providers/klingai.
package klingai
