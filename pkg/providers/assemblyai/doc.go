// Package assemblyai is the provider for AssemblyAI speech-to-text and
// text-to-speech. Transcription runs as an asynchronous job that the provider
// submits and polls; speech is one buffered request.
//
// Create the provider, then ask it for a model and pass the model to
// ai.Transcribe:
//
//	p := assemblyai.New(assemblyai.Config{
//		APIKey: os.Getenv("ASSEMBLYAI_API_KEY"),
//	})
//	model, err := p.TranscriptionModel(assemblyai.ModelAssemblyAISTT37)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/assemblyai.
package assemblyai
