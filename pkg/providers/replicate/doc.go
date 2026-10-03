// Package replicate is the provider for Replicate, a marketplace for open-
// source models. Model IDs are Replicate model names such as "owner/model". It
// supports language, image and video models.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := replicate.New(replicate.Config{
//		APIKey: os.Getenv("REPLICATE_API_KEY"),
//	})
//	model, err := p.LanguageModel("meta/meta-llama-3-70b-instruct")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/replicate.
package replicate
