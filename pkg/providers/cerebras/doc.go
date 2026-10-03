// Package cerebras is the provider for Cerebras, which serves open-weight chat
// models on wafer-scale hardware at very high speed.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := cerebras.New(cerebras.Config{
//		APIKey: os.Getenv("CEREBRAS_API_KEY"),
//	})
//	model, err := p.LanguageModel(cerebras.ModelGPTOSS120B)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/cerebras.
package cerebras
