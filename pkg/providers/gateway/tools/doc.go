// Package tools provides provider tools for the Vercel AI Gateway: search and
// fetch tools that the gateway runs for you, backed by services such as Exa,
// Parallel, Perplexity, Tako and Browserbase. Each constructor returns a tool
// you pass in the Tools list of ai.GenerateText, ai.StreamText or an agent.
//
//	search := tools.NewExaSearch(tools.ExaSearchConfig{Type: "auto"})
//	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
//		Model:  model, // a model from the gateway provider
//		Prompt: "What changed in Go 1.26?",
//		Tools:  []types.Tool{types.Tool(search)},
//	})
//
// The tools only work with models served by the gateway provider.
//
// Guide: https://goaisdk.com/docs/providers/gateway.
package tools
