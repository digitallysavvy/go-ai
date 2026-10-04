// Package assemblyai is the provider for AssemblyAI speech-to-text.
// Transcription runs as an asynchronous job that the provider submits and
// polls.
//
// Create the provider, then ask it for a model and pass the model to
// ai.Transcribe:
//
//	p := assemblyai.New(assemblyai.Config{
//		APIKey: os.Getenv("ASSEMBLYAI_API_KEY"),
//	})
//	model, err := p.TranscriptionModel(assemblyai.ModelUniversal3Pro)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/assemblyai.
package assemblyai
