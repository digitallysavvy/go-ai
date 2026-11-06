// Package heygen is the provider for HeyGen video generation. It supports
// text-to-video, image-to-video, and reference-to-video generation with
// heygen-video-1; generated clips include audio.
//
// Create the provider, ask it for a video model, and pass the model to
// ai.GenerateVideo:
//
//	p := heygen.New(heygen.Config{
//		APIKey: os.Getenv("HEYGEN_API_KEY"),
//	})
//	model, err := p.VideoModel("heygen-video-1")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// The model implements the asynchronous DoStart/DoStatus protocol; polling is
// orchestrated by the caller (ai.GenerateVideo) or by DoGenerate. HeyGen's
// avatar, voice, translation, and video editing APIs are not included.
//
// Guide: https://goaisdk.com/docs/providers/heygen.
//
// Provenance: this package mirrors @ai-sdk/heygen from the Vercel AI SDK for
// TypeScript.
package heygen
