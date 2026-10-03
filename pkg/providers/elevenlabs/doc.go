// Package elevenlabs is the provider for ElevenLabs text-to-speech and speech-
// to-text (Scribe) models.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateSpeech:
//
//	p := elevenlabs.New(elevenlabs.Config{
//		APIKey: os.Getenv("ELEVENLABS_API_KEY"),
//	})
//	model, err := p.SpeechModel("eleven_multilingual_v2")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/elevenlabs.
package elevenlabs
