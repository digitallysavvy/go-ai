// Package groq is the provider for Groq, which runs chat and speech-to-text
// models on its LPU hardware for low-latency inference.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := groq.New(groq.Config{
//		APIKey: os.Getenv("GROQ_API_KEY"),
//	})
//	model, err := p.LanguageModel("llama-3.3-70b-versatile")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/groq.
package groq
