package telemetry

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS 382d0b8eaf exports the GenAI formatting helpers from @ai-sdk/otel.
// These tests port otel/src/gen-ai-format-messages.test.ts against the
// exported Go API. Each expectation is the JSON.stringify output of the TS
// result (TS object literal key order), compared byte for byte.

func r89carryJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(b)
}

func r89carryExpect(t *testing.T, v interface{}, want string) {
	t.Helper()
	if got := r89carryJSON(t, v); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func r89carryUser(text string) types.Message {
	return types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}}
}

func r89carrySystem(text string) types.Message {
	return types.Message{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: text}}}
}

// formatSystemInstructions: 'should format a system string into SemConv system instructions'.
func TestR89carryFormatSystemInstructions(t *testing.T) {
	r89carryExpect(t, FormatSystemInstructions("You are a helpful assistant."),
		`[{"type":"text","content":"You are a helpful assistant."}]`)
	// TS returns one text instruction for any string, the empty one included.
	r89carryExpect(t, FormatSystemInstructions(""), `[{"type":"text","content":""}]`)
}

// TS formatSystemInstructions with a system message array: one text
// instruction per message.
func TestR89carryFormatSystemMessages(t *testing.T) {
	r89carryExpect(t, FormatSystemMessages([]types.Message{r89carrySystem("A"), r89carrySystem("B")}),
		`[{"type":"text","content":"A"},{"type":"text","content":"B"}]`)
}

func TestR89carryFormatInputMessages(t *testing.T) {
	cases := []struct {
		name   string
		prompt []types.Message
		want   string
	}{
		{
			name:   "should convert user text messages",
			prompt: []types.Message{r89carryUser("What is the weather?")},
			want:   `[{"role":"user","parts":[{"type":"text","content":"What is the weather?"}]}]`,
		},
		{
			name:   "should preserve system messages in prompt order",
			prompt: []types.Message{r89carryUser("First"), r89carrySystem("Be helpful"), r89carryUser("Second")},
			want:   `[{"role":"user","parts":[{"type":"text","content":"First"}]},{"role":"system","parts":[{"type":"text","content":"Be helpful"}]},{"role":"user","parts":[{"type":"text","content":"Second"}]}]`,
		},
		{
			name: "should convert assistant messages with tool calls",
			prompt: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{
				ToolCallID: "call_123", ToolName: "get_weather", Arguments: map[string]interface{}{"city": "Paris"},
			}}}},
			want: `[{"role":"assistant","parts":[{"type":"tool_call","id":"call_123","name":"get_weather","arguments":{"city":"Paris"}}]}]`,
		},
		{
			name: "should convert tool result messages",
			prompt: []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
				ToolCallID: "call_123", ToolName: "get_weather",
				Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "Sunny, 72°F"},
			}}}},
			want: `[{"role":"tool","parts":[{"type":"tool_call_response","id":"call_123","response":"Sunny, 72°F"}]}]`,
		},
		{
			name: "should convert file parts to blob parts",
			prompt: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{
				FileData: types.FileData{Type: types.FileDataTypeData, DataString: "base64data"}, MediaType: "image/png",
			}}}},
			want: `[{"role":"user","parts":[{"type":"blob","modality":"image","mime_type":"image/png","content":"base64data"}]}]`,
		},
		{
			name: "should convert URL file parts to uri parts",
			prompt: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{
				FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/image.png"}, MediaType: "image/png",
			}}}},
			want: `[{"role":"user","parts":[{"type":"uri","modality":"image","mime_type":"image/png","uri":"https://example.com/image.png"}]}]`,
		},
		{
			name:   "should convert reasoning parts",
			prompt: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{types.ReasoningContent{Text: "Let me think about this..."}}}},
			want:   `[{"role":"assistant","parts":[{"type":"reasoning","content":"Let me think about this..."}]}]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r89carryExpect(t, FormatInputMessages(tc.prompt), tc.want)
		})
	}
}

func r89carryPNG(base64 string, mediaType string) types.GeneratedFileContent {
	return types.GeneratedFileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeData, DataString: base64}}
}

func TestR89carryFormatOutputMessages(t *testing.T) {
	cases := []struct {
		name string
		opts FormatOutputMessagesOptions
		want string
	}{
		{
			name: "should format text-only output",
			opts: FormatOutputMessagesOptions{Text: "Hello world", FinishReason: "stop"},
			want: `[{"role":"assistant","parts":[{"type":"text","content":"Hello world"}],"finish_reason":"stop"}]`,
		},
		{
			name: "should format output with reasoning",
			opts: FormatOutputMessagesOptions{Text: "The answer is 42", Reasoning: []types.ReasoningContent{{Text: "Let me think..."}}, FinishReason: "stop"},
			want: `[{"role":"assistant","parts":[{"type":"reasoning","content":"Let me think..."},{"type":"text","content":"The answer is 42"}],"finish_reason":"stop"}]`,
		},
		{
			name: "should format output with tool calls",
			opts: FormatOutputMessagesOptions{
				ToolCalls:    []types.ToolCall{{ID: "call_abc", ToolName: "get_weather", Arguments: map[string]interface{}{"city": "Paris"}}},
				FinishReason: "tool-calls",
			},
			want: `[{"role":"assistant","parts":[{"type":"tool_call","id":"call_abc","name":"get_weather","arguments":{"city":"Paris"}}],"finish_reason":"tool_call"}]`,
		},
		{
			name: "should format output with tool results",
			opts: FormatOutputMessagesOptions{
				ToolResults:  []types.ToolResult{{ToolCallID: "call_abc", Result: map[string]interface{}{"temperature": 21}}},
				FinishReason: "stop",
			},
			want: `[{"role":"assistant","parts":[{"type":"tool_call_response","id":"call_abc","response":{"temperature":21}}],"finish_reason":"stop"}]`,
		},
		{
			name: "should format output with files",
			opts: FormatOutputMessagesOptions{Files: []types.GeneratedFileContent{r89carryPNG("abc123", "image/png")}, FinishReason: "stop"},
			want: `[{"role":"assistant","parts":[{"type":"blob","modality":"image","mime_type":"image/png","content":"abc123"}],"finish_reason":"stop"}]`,
		},
		{
			name: "should combine reasoning, text, tool calls, and files",
			opts: FormatOutputMessagesOptions{
				Text:         "Here is the result",
				Reasoning:    []types.ReasoningContent{{Text: "Thinking..."}},
				ToolCalls:    []types.ToolCall{{ID: "tc1", ToolName: "search", Arguments: map[string]interface{}{"q": "test"}}},
				Files:        []types.GeneratedFileContent{r89carryPNG("data", "image/jpeg")},
				FinishReason: "stop",
			},
			want: `[{"role":"assistant","parts":[{"type":"reasoning","content":"Thinking..."},{"type":"text","content":"Here is the result"},{"type":"tool_call","id":"tc1","name":"search","arguments":{"q":"test"}},{"type":"blob","modality":"image","mime_type":"image/jpeg","content":"data"}],"finish_reason":"stop"}]`,
		},
		{
			// TS: `'text' in r && r.text` skips empty reasoning; an empty text adds no part.
			name: "empty reasoning and text add no parts",
			opts: FormatOutputMessagesOptions{Reasoning: []types.ReasoningContent{{Text: ""}}, FinishReason: "other"},
			want: `[{"role":"assistant","parts":[],"finish_reason":"stop"}]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r89carryExpect(t, FormatOutputMessages(tc.opts), tc.want)
		})
	}

	t.Run("should map finish reasons correctly", func(t *testing.T) {
		for reason, want := range map[string]string{"stop": "stop", "length": "length", "tool-calls": "tool_call", "content-filter": "content_filter"} {
			if got := FormatOutputMessages(FormatOutputMessagesOptions{FinishReason: reason})[0].FinishReason; got != want {
				t.Errorf("%s: got %q, want %q", reason, got, want)
			}
		}
	})
}

// formatObjectOutputMessages: 'should format object output as text content'.
func TestR89carryFormatObjectOutputMessages(t *testing.T) {
	r89carryExpect(t, FormatObjectOutputMessages(`{"name":"test"}`, "stop"),
		`[{"role":"assistant","parts":[{"type":"text","content":"{\"name\":\"test\"}"}],"finish_reason":"stop"}]`)
}

func TestR89carryFormatModelMessages(t *testing.T) {
	toolCallMsg := types.Message{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{
		ToolCallID: "tc1", ToolName: "weather", Arguments: map[string]interface{}{"city": "NYC"},
	}}}
	cases := []struct {
		name string
		opts FormatModelMessagesOptions
		want string
	}{
		{
			name: "should convert a prompt string to a user message",
			opts: FormatModelMessagesOptions{Prompt: "Hello"},
			want: `[{"role":"user","parts":[{"type":"text","content":"Hello"}]}]`,
		},
		{
			// TS's array form of prompt maps to Messages.
			name: "should convert ModelMessage array from prompt",
			opts: FormatModelMessagesOptions{Messages: []types.Message{r89carryUser("Hi there"), toolCallMsg}},
			want: `[{"role":"user","parts":[{"type":"text","content":"Hi there"}]},{"role":"assistant","parts":[{"type":"tool_call","id":"tc1","name":"weather","arguments":{"city":"NYC"}}]}]`,
		},
		{
			name: "should preserve system messages in message order",
			opts: FormatModelMessagesOptions{Messages: []types.Message{r89carryUser("First"), r89carrySystem("Be helpful"), r89carryUser("Second")}},
			want: `[{"role":"user","parts":[{"type":"text","content":"First"}]},{"role":"system","parts":[{"type":"text","content":"Be helpful"}]},{"role":"user","parts":[{"type":"text","content":"Second"}]}]`,
		},
		{
			name: "should convert tool messages",
			opts: FormatModelMessagesOptions{Messages: []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
				ToolCallID: "tc1", ToolName: "weather", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "Sunny"},
			}}}}},
			want: `[{"role":"tool","parts":[{"type":"tool_call_response","id":"tc1","response":"Sunny"}]}]`,
		},
		{
			name: "should combine prompt and messages",
			opts: FormatModelMessagesOptions{Prompt: "First message", Messages: []types.Message{r89carryUser("Second message")}},
			want: `[{"role":"user","parts":[{"type":"text","content":"First message"}]},{"role":"user","parts":[{"type":"text","content":"Second message"}]}]`,
		},
		{
			name: "should return empty array when both prompt and messages are undefined",
			opts: FormatModelMessagesOptions{},
			want: `[]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r89carryExpect(t, FormatModelMessages(tc.opts), tc.want)
		})
	}
}

// TS open-telemetry.ts builds the step and root gen_ai.output.messages with
// formatOutputMessages, which writes `id: tc.toolCallId` and `mime_type:
// file.mediaType` as given: an empty tool call id or media type stays "",
// not null.
func TestR89carryOutputMessagesKeepEmptyIDs(t *testing.T) {
	content := []types.ContentPart{
		types.ToolCallContent{ToolCallID: "", ToolName: "t", Arguments: map[string]interface{}{}},
		types.ToolResultContent{ToolCallID: "", ToolName: "t", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "r"}},
		types.GeneratedFileContent{FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AA=="}},
	}
	want := `[{"role":"assistant","parts":[{"type":"tool_call","id":"","name":"t","arguments":{}},{"type":"tool_call_response","id":"","response":"r"},{"type":"blob","modality":"image","mime_type":"","content":"AA=="}],"finish_reason":"tool_call"}]`
	r89carryExpect(t, formatOutputMessagesFromContent(content, "tool-calls"), want)
	r89carryExpect(t, formatOutputMessagesFromFinish(TelemetryFinishEvent{
		ToolCalls:    []types.ToolCall{{ID: "", ToolName: "t", Arguments: map[string]interface{}{}}},
		ToolResults:  []types.ToolResult{{ToolCallID: "", Result: "r"}},
		Files:        []types.GeneratedFileContent{{FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AA=="}}},
		FinishReason: "tool-calls",
	}), want)
}
