// Package deepseek is the provider for DeepSeek chat and reasoning models.
// Reasoning output is returned as reasoning content, separate from the answer
// text.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := deepseek.New(deepseek.Config{
//		APIKey: os.Getenv("DEEPSEEK_API_KEY"),
//	})
//	model, err := p.LanguageModel(deepseek.ModelDeepSeekV4Pro)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/deepseek.
package deepseek
