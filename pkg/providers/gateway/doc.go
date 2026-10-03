// Package gateway is the provider for the Vercel AI Gateway, which routes one
// request format to models from many providers. Model IDs have the form
// "provider/model". The gateway handles provider selection, failover and cost
// reporting.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p, err := gateway.New(gateway.Config{
//		APIKey: os.Getenv("AI_GATEWAY_API_KEY"),
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	model, err := p.LanguageModel("anthropic/claude-sonnet-5.5")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// GetAvailableModels lists the models the gateway offers and GetCredits
// reports your balance. Subpackages gateway/errors and gateway/tools hold the
// gateway error types and its provider tools.
//
// Guide: https://goaisdk.com/docs/providers/gateway.
package gateway
