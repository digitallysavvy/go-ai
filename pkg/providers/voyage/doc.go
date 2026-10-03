// Package voyage is the provider for Voyage AI embedding and reranking models,
// which are tuned for retrieval.
//
// Create the provider, then ask it for a model and pass the model to ai.Embed:
//
//	p := voyage.New(voyage.Config{
//		APIKey: os.Getenv("VOYAGE_API_KEY"),
//	})
//	model, err := p.EmbeddingModel(voyage.ModelVoyage4)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/voyage.
package voyage
