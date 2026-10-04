// Package moonshot is the provider for Moonshot AI, the maker of the Kimi chat
// models. It supports tool use and reasoning.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := moonshot.New(moonshot.Config{
//		APIKey: os.Getenv("MOONSHOT_API_KEY"),
//	})
//	model, err := p.LanguageModel("kimi-k2-0905")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/moonshot.
package moonshot
