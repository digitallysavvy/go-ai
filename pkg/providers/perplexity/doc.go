// Package perplexity is the provider for the Perplexity Agent API. It combines
// model reasoning with live web search and returns cited answers. The package
// also supports the legacy Sonar models and embeddings.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := perplexity.New(perplexity.Config{
//		APIKey: os.Getenv("PERPLEXITY_API_KEY"),
//	})
//	model, err := p.LanguageModel("sonar-pro")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/perplexity.
package perplexity
