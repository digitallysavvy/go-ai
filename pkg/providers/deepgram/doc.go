// Package deepgram is the provider for Deepgram speech-to-text (Nova models)
// and text-to-speech (Aura voices).
//
// Create the provider, then ask it for a model and pass the model to
// ai.Transcribe:
//
//	p := deepgram.New(deepgram.Config{
//		APIKey: os.Getenv("DEEPGRAM_API_KEY"),
//	})
//	model, err := p.TranscriptionModel("nova-3")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/deepgram.
package deepgram
