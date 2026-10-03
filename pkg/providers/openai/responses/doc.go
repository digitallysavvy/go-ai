// Package responses holds the OpenAI Responses API building blocks that
// package openai uses for ResponsesModel: converting a prompt to Responses API
// input, preparing tools, decoding streams, and the client-side helpers for
// computer use, local shell and programmatic tool calling. Most applications
// call openai.Provider.ResponsesModel and never import this package.
//
// Import it when you need a helper directly, for example the computer tool:
//
//	computer := responses.NewComputerTool()
//
// Computer tool flow: include the tool in the request, execute the UI actions
// the model returns, then send back a screenshot as the tool result.
//
// Guide: https://goaisdk.com/docs/providers/openai.
package responses
