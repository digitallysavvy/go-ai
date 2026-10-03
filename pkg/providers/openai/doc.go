// Package openai is the provider for OpenAI models: GPT and reasoning models
// for chat and the Responses API, plus embeddings, image generation, speech,
// transcription, files and batches. Other OpenAI-compatible providers in this
// SDK build on it.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := openai.New(openai.Config{
//		APIKey: os.Getenv("OPENAI_API_KEY"),
//	})
//	model, err := p.LanguageModel(openai.ModelGPT6Astra)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// LanguageModel returns a Responses API model, which suits provider tools such
// as web search, file search and computer use. Use ChatModel for the Chat
// Completions API. Set Config.BaseURL to use any OpenAI-compatible endpoint.
//
// Guide: https://goaisdk.com/docs/providers/openai.
package openai
