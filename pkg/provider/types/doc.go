// Package types holds the data types shared by the SDK and by providers:
// messages and content parts, tools and tool calls, usage, finish reasons,
// results, and provider metadata.
//
// You use these types when you build prompts and tools:
//
//	messages := []types.Message{
//		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
//	}
//
//	weather := types.Tool{
//		Name:        "get_weather",
//		Description: "Get the current weather for a city",
//		Parameters: map[string]interface{}{
//			"type":       "object",
//			"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
//			"required":   []string{"city"},
//		},
//		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
//			return "sunny", nil
//		},
//	}
//
// The types you meet most often:
//
//   - Message and ContentPart (TextContent, ImageContent, ToolCall and so on).
//   - Tool, ToolCall, ToolResult, ToolExecutionOptions.
//   - GenerateResult and StepResult: what a model returns for one call or step.
//   - Usage and FinishReason.
//
// Reference: https://goaisdk.com/docs/reference/types/messages and
// https://goaisdk.com/docs/reference/types/tools.
//
// These types follow the shapes of the language model specification used by
// the Vercel AI SDK for TypeScript, so data moves between the two SDKs
// without translation.
package types
