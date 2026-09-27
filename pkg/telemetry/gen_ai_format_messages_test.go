package telemetry

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Golden tests ported from TS's otel/src/gen-ai-format-messages.test.ts.
// Each test compares the JSON-round-tripped Go output against a JSON literal
// matching the TS `toMatchInlineSnapshot` expectation for the same input, so
// key spelling/shape (not Go map ordering) is what's under test.

func jsonRoundTrip(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return out
}

func mustParseJSON(t *testing.T, s string) interface{} {
	t.Helper()
	var out interface{}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", s, err)
	}
	return out
}

func assertJSONEqual(t *testing.T, got interface{}, wantJSON string) {
	t.Helper()
	gotRT := jsonRoundTrip(t, got)
	want := mustParseJSON(t, wantJSON)
	if !reflect.DeepEqual(gotRT, want) {
		gotB, _ := json.MarshalIndent(gotRT, "", "  ")
		wantB, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("mismatch:\ngot:  %s\nwant: %s", gotB, wantB)
	}
}

func TestMapProviderNameKnownProviders(t *testing.T) {
	got := map[string]string{
		"anthropic":    mapProviderName("anthropic.messages"),
		"openai":       mapProviderName("openai.chat"),
		"googleGemini": mapProviderName("google.generative-ai"),
		"mistral":      mapProviderName("mistral.chat"),
		"groq":         mapProviderName("groq.chat"),
		"deepseek":     mapProviderName("deepseek.chat"),
	}
	assertJSONEqual(t, got, `{
		"anthropic": "anthropic",
		"deepseek": "deepseek",
		"googleGemini": "gcp.gemini",
		"groq": "groq",
		"mistral": "mistral_ai",
		"openai": "openai"
	}`)
}

func TestMapProviderNameGoogleVertex(t *testing.T) {
	got := map[string]string{
		"vertexChat":      mapProviderName("google.vertex.chat"),
		"vertexEmbedding": mapProviderName("google.vertex.embedding"),
		"vertexImage":     mapProviderName("google.vertex.image"),
		"googleVertex":    mapProviderName("google-vertex"),
	}
	assertJSONEqual(t, got, `{
		"googleVertex": "gcp.vertex_ai",
		"vertexChat": "gcp.vertex_ai",
		"vertexEmbedding": "gcp.vertex_ai",
		"vertexImage": "gcp.vertex_ai"
	}`)
}

func TestMapProviderNameBareGoogle(t *testing.T) {
	if got := mapProviderName("google.chat"); got != "gcp.gemini" {
		t.Fatalf("mapProviderName(%q) = %q, want gcp.gemini", "google.chat", got)
	}
}

func TestMapProviderNameBedrock(t *testing.T) {
	got := map[string]string{
		"amazonBedrock": mapProviderName("amazon-bedrock.chat"),
		"bedrock":       mapProviderName("bedrock.chat"),
	}
	assertJSONEqual(t, got, `{"amazonBedrock": "aws.bedrock", "bedrock": "aws.bedrock"}`)
}

func TestMapProviderNameAzure(t *testing.T) {
	got := map[string]string{
		"azureChat":   mapProviderName("azure.chat"),
		"azureOpenai": mapProviderName("azure-openai.chat"),
	}
	assertJSONEqual(t, got, `{"azureChat": "azure.ai.inference", "azureOpenai": "azure.ai.openai"}`)
}

func TestMapProviderNameUnknown(t *testing.T) {
	if got := mapProviderName("custom-provider.chat"); got != "custom-provider.chat" {
		t.Fatalf("mapProviderName(%q) = %q, want unchanged", "custom-provider.chat", got)
	}
}

func TestMapOperationNameGenerateStream(t *testing.T) {
	got := map[string]string{
		"generateText": mapOperationName("ai.generateText"),
		"streamText":   mapOperationName("ai.streamText"),
	}
	assertJSONEqual(t, got, `{"generateText": "invoke_agent", "streamText": "invoke_agent"}`)
}

func TestMapOperationNameObject(t *testing.T) {
	got := map[string]string{
		"generateObject": mapOperationName("ai.generateObject"),
		"streamObject":   mapOperationName("ai.streamObject"),
	}
	assertJSONEqual(t, got, `{"generateObject": "invoke_agent", "streamObject": "invoke_agent"}`)
}

func TestMapOperationNameEmbed(t *testing.T) {
	got := map[string]string{
		"embed":     mapOperationName("ai.embed"),
		"embedMany": mapOperationName("ai.embedMany"),
	}
	assertJSONEqual(t, got, `{"embed": "embeddings", "embedMany": "embeddings"}`)
}

func TestMapOperationNameRerank(t *testing.T) {
	if got := mapOperationName("ai.rerank"); got != "rerank" {
		t.Fatalf("mapOperationName(ai.rerank) = %q, want rerank", got)
	}
}

func TestMapOperationNameUnknown(t *testing.T) {
	if got := mapOperationName("ai.unknown"); got != "ai.unknown" {
		t.Fatalf("mapOperationName(ai.unknown) = %q, want unchanged", got)
	}
}

func TestFormatSystemInstructions(t *testing.T) {
	assertJSONEqual(t, formatSystemInstructions("You are a helpful assistant."), `[
		{"content": "You are a helpful assistant.", "type": "text"}
	]`)
}

func TestFormatSystemInstructionsEmpty(t *testing.T) {
	if got := formatSystemInstructions(""); got != nil {
		t.Fatalf("formatSystemInstructions(\"\") = %v, want nil", got)
	}
}

func textMessage(role types.MessageRole, text string) types.Message {
	return types.Message{Role: role, Content: []types.ContentPart{types.TextContent{Text: text}}}
}

func TestFormatInputMessagesUserText(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "What is the weather?"}}},
	}
	assertJSONEqual(t, formatInputMessages(messages), `[
		{"parts": [{"content": "What is the weather?", "type": "text"}], "role": "user"}
	]`)
}

func TestFormatInputMessagesPreservesSystemOrder(t *testing.T) {
	messages := []types.Message{
		textMessage(types.RoleUser, "First"),
		textMessage(types.RoleSystem, "Be helpful"),
		textMessage(types.RoleUser, "Second"),
	}
	assertJSONEqual(t, formatInputMessages(messages), `[
		{"parts": [{"content": "First", "type": "text"}], "role": "user"},
		{"parts": [{"content": "Be helpful", "type": "text"}], "role": "system"},
		{"parts": [{"content": "Second", "type": "text"}], "role": "user"}
	]`)
}

func TestFormatInputMessagesToolCall(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ToolCallContent{
					ToolCallID: "call_123",
					ToolName:   "get_weather",
					Arguments:  map[string]interface{}{"city": "Paris"},
				},
			},
		},
	}
	assertJSONEqual(t, formatInputMessages(messages), `[
		{"parts": [{"arguments": {"city": "Paris"}, "id": "call_123", "name": "get_weather", "type": "tool_call"}], "role": "assistant"}
	]`)
}

func TestFormatInputMessagesToolResult(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "call_123",
					ToolName:   "get_weather",
					Output:     &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "Sunny, 72°F"},
				},
			},
		},
	}
	assertJSONEqual(t, formatInputMessages(messages), `[
		{"parts": [{"id": "call_123", "response": "Sunny, 72°F", "type": "tool_call_response"}], "role": "tool"}
	]`)
}

func TestFormatInputMessagesFileDataBlob(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.FileContent{
					FileData:  types.FileData{Type: types.FileDataTypeData, DataString: "base64data"},
					MediaType: "image/png",
				},
			},
		},
	}
	assertJSONEqual(t, formatInputMessages(messages), `[
		{"parts": [{"content": "base64data", "mime_type": "image/png", "modality": "image", "type": "blob"}], "role": "user"}
	]`)
}

func TestFormatInputMessagesFileURLUri(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.FileContent{
					FileData:  types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/image.png"},
					MediaType: "image/png",
				},
			},
		},
	}
	assertJSONEqual(t, formatInputMessages(messages), `[
		{"parts": [{"mime_type": "image/png", "modality": "image", "type": "uri", "uri": "https://example.com/image.png"}], "role": "user"}
	]`)
}

func TestFormatInputMessagesReasoning(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ReasoningContent{Text: "Let me think about this..."},
			},
		},
	}
	assertJSONEqual(t, formatInputMessages(messages), `[
		{"parts": [{"content": "Let me think about this...", "type": "reasoning"}], "role": "assistant"}
	]`)
}

func TestFormatOutputMessagesTextOnly(t *testing.T) {
	content := []types.ContentPart{types.TextContent{Text: "Hello world"}}
	assertJSONEqual(t, formatOutputMessagesFromContent(content, "stop"), `[
		{"finish_reason": "stop", "parts": [{"content": "Hello world", "type": "text"}], "role": "assistant"}
	]`)
}

func TestFormatOutputMessagesWithReasoning(t *testing.T) {
	content := []types.ContentPart{
		types.ReasoningContent{Text: "Let me think..."},
		types.TextContent{Text: "The answer is 42"},
	}
	assertJSONEqual(t, formatOutputMessagesFromContent(content, "stop"), `[
		{"finish_reason": "stop", "parts": [
			{"content": "Let me think...", "type": "reasoning"},
			{"content": "The answer is 42", "type": "text"}
		], "role": "assistant"}
	]`)
}

func TestFormatOutputMessagesWithToolCalls(t *testing.T) {
	content := []types.ContentPart{
		types.ToolCallContent{ToolCallID: "call_abc", ToolName: "get_weather", Arguments: map[string]interface{}{"city": "Paris"}},
	}
	assertJSONEqual(t, formatOutputMessagesFromContent(content, "tool-calls"), `[
		{"finish_reason": "tool_call", "parts": [
			{"arguments": {"city": "Paris"}, "id": "call_abc", "name": "get_weather", "type": "tool_call"}
		], "role": "assistant"}
	]`)
}

func TestFormatOutputMessagesWithToolResults(t *testing.T) {
	content := []types.ContentPart{
		types.ToolResultContent{
			ToolCallID: "call_abc",
			Output:     &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"temperature": 21.0}},
		},
	}
	assertJSONEqual(t, formatOutputMessagesFromContent(content, "stop"), `[
		{"finish_reason": "stop", "parts": [
			{"id": "call_abc", "response": {"temperature": 21}, "type": "tool_call_response"}
		], "role": "assistant"}
	]`)
}

func TestFormatOutputMessagesWithFiles(t *testing.T) {
	content := []types.ContentPart{
		types.GeneratedFileContent{MediaType: "image/png", Data: []byte("abc123")},
	}
	got := formatOutputMessagesFromContent(content, "stop")
	msgs, ok := got[0].Parts[0]["content"].(string)
	if !ok || msgs == "" {
		t.Fatalf("expected non-empty base64 content, got %#v", got[0].Parts[0])
	}
	if got[0].Parts[0]["type"] != "blob" || got[0].Parts[0]["mime_type"] != "image/png" || got[0].Parts[0]["modality"] != "image" {
		t.Fatalf("unexpected file part shape: %#v", got[0].Parts[0])
	}
}

func TestFormatOutputMessagesCombined(t *testing.T) {
	content := []types.ContentPart{
		types.ReasoningContent{Text: "Thinking..."},
		types.TextContent{Text: "Here is the result"},
		types.ToolCallContent{ToolCallID: "tc1", ToolName: "search", Arguments: map[string]interface{}{"q": "test"}},
	}
	assertJSONEqual(t, formatOutputMessagesFromContent(content, "stop"), `[
		{"finish_reason": "stop", "parts": [
			{"content": "Thinking...", "type": "reasoning"},
			{"content": "Here is the result", "type": "text"},
			{"arguments": {"q": "test"}, "id": "tc1", "name": "search", "type": "tool_call"}
		], "role": "assistant"}
	]`)
}

func TestMapFinishReasonSemConv(t *testing.T) {
	got := map[string]string{
		"stop":          mapFinishReasonSemConv("stop"),
		"length":        mapFinishReasonSemConv("length"),
		"toolCalls":     mapFinishReasonSemConv("tool-calls"),
		"contentFilter": mapFinishReasonSemConv("content-filter"),
	}
	assertJSONEqual(t, got, `{
		"contentFilter": "content_filter",
		"length": "length",
		"stop": "stop",
		"toolCalls": "tool_call"
	}`)
}

func TestFormatObjectOutputMessages(t *testing.T) {
	assertJSONEqual(t, formatObjectOutputMessages(`{"name":"test"}`, "stop"), `[
		{"finish_reason": "stop", "parts": [{"content": "{\"name\":\"test\"}", "type": "text"}], "role": "assistant"}
	]`)
}

func TestFormatInputMessagesEmpty(t *testing.T) {
	if got := formatInputMessages(nil); len(got) != 0 {
		t.Fatalf("formatInputMessages(nil) = %v, want empty", got)
	}
}
