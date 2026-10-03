// Package ai is the main entry point of the Go AI SDK. It provides the
// functions you call to generate text, stream text, produce structured
// objects, call tools, and work with embeddings, images, speech,
// transcription, video, and reranking, all against any provider.
//
// You pick a provider (OpenAI, Anthropic, Google, and many more under
// pkg/providers), ask it for a model, and pass that model to one of the
// functions in this package. The same call works with every provider.
//
// # Generate text
//
//	provider := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
//	model, err := provider.LanguageModel(openai.ModelGPT6Astra)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
//		Model:  model,
//		Prompt: "What is an agent?",
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Println(result.Text)
//
// # Stream text
//
// StreamText returns as soon as the request starts. Read the chunks as they
// arrive:
//
//	stream, err := ai.StreamText(ctx, ai.StreamTextOptions{Model: model, Prompt: "Tell me a story."})
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer stream.Close()
//	for chunk := range stream.Chunks() {
//		if chunk.Type == provider.ChunkTypeText {
//			fmt.Print(chunk.Text)
//		}
//	}
//
// To serve a chat UI built with the AI SDK useChat hook, pass the stream to
// PipeUIMessageStreamToResponse together with your http.ResponseWriter.
//
// # Other functions
//
//   - GenerateObject, GenerateObjectInto, StreamObject: structured output that
//     matches a schema (see package schema).
//   - Tools: set GenerateTextOptions.Tools to a list of types.Tool values and
//     StopWhen to limit the number of steps.
//   - Embed, EmbedMany, Rerank: embeddings and reranking.
//   - GenerateImage, GenerateSpeech, Transcribe, GenerateVideo: media models.
//   - WrapLanguageModel: add middleware such as caching or logging.
//
// Package testutil provides mock models, so you can test code that calls this
// package without network access. The Example functions in this package show
// each call end to end.
//
// Guides and reference: https://goaisdk.com/docs/ai-sdk-core/generating-text
// and https://goaisdk.com/docs/reference/ai/generate-text.
//
// This package is the Go counterpart of the "ai" package in the Vercel AI SDK
// for TypeScript, and names and behavior follow it where Go allows.
package ai
