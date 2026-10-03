// Package tool converts SDK tools and tool choices to the formats providers
// expect: ToOpenAIFormat, ToAnthropicFormat, ToGoogleFormat and the matching
// ConvertToolChoice functions. It also validates tool calls (FindTool,
// ValidateToolCall) and parses tool call arguments. Provider authors use it.
// Application code rarely needs it.
package tool
