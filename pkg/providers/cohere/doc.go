// Package cohere is the provider for Cohere. It offers the Command chat
// models, Embed embedding models and Rerank reranking models, which suit
// retrieval and RAG pipelines.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := cohere.New(cohere.Config{
//		APIKey: os.Getenv("COHERE_API_KEY"),
//	})
//	model, err := p.LanguageModel("command-a-03-2025")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/cohere.
package cohere
