package openresponses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestConvertAssistantContent_ReasoningWithEncryptedContent verifies that a
// ReasoningContent part with EncryptedContent is emitted as a top-level
// ReasoningInputItem (not as output_text) so the API can use it in the next
// turn (#12869 input-side fix).
func TestConvertAssistantContent_ReasoningWithEncryptedContent(t *testing.T) {
	content := []types.ContentPart{
		types.ReasoningContent{
			Text:             "some thinking",
			EncryptedContent: "enc-xyz789",
		},
	}

	items := convertAssistantContent(content, "openai", false)

	// One item should be emitted, and it should not be a text message.
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d: %+v", len(items), items)
	}

	item, ok := items[0].(ReasoningInputItem)
	if !ok {
		t.Fatalf("expected ReasoningInputItem, got %T", items[0])
	}
	if item.Type != "reasoning" {
		t.Errorf("item.Type = %q, want %q", item.Type, "reasoning")
	}
	if item.EncryptedContent != "enc-xyz789" {
		t.Errorf("item.EncryptedContent = %q, want %q", item.EncryptedContent, "enc-xyz789")
	}
	// No reasoningSummary provider data was present, so summary stays empty
	// (TS never derives `summary` from part.text — only `content` does).
	if len(item.Summary) != 0 {
		t.Errorf("expected empty summary, got %+v", item.Summary)
	}
	if len(item.Content) != 1 || item.Content[0].Text != "some thinking" || item.Content[0].Type != "reasoning_text" {
		t.Errorf("unexpected content: %+v", item.Content)
	}
}

// TestConvertAssistantContent_ReasoningWithoutEncryptedContentIsStillEmitted
// verifies that a ReasoningContent part without EncryptedContent (e.g. from a
// provider that doesn't use encrypted_content) is still emitted as a
// reasoning input item — the TS SDK never drops reasoning parts here
// (OR-CORE item 4/5).
func TestConvertAssistantContent_ReasoningWithoutEncryptedContentIsStillEmitted(t *testing.T) {
	content := []types.ContentPart{
		types.ReasoningContent{
			Text: "anthropic thinking — no encrypted content",
		},
		types.TextContent{Text: "final answer"},
	}

	items := convertAssistantContent(content, "openai", false)
	if len(items) != 2 {
		t.Fatalf("expected 2 items (reasoning + message), got %d: %+v", len(items), items)
	}

	reasoning, ok := items[0].(ReasoningInputItem)
	if !ok {
		t.Fatalf("items[0] = %T, want ReasoningInputItem", items[0])
	}
	if reasoning.EncryptedContent != "" {
		t.Errorf("EncryptedContent = %q, want empty", reasoning.EncryptedContent)
	}
	if len(reasoning.Summary) != 0 {
		t.Errorf("expected empty summary, got %+v", reasoning.Summary)
	}
	if len(reasoning.Content) != 1 || reasoning.Content[0].Text != "anthropic thinking — no encrypted content" {
		t.Errorf("unexpected content: %+v", reasoning.Content)
	}

	message, ok := items[1].(MessageItem)
	if !ok || message.Role != "assistant" {
		t.Fatalf("items[1] = %#v, want assistant message", items[1])
	}
}

// TestConvertAssistantContent_ReasoningEmptySummaryWhenNoText verifies that when
// EncryptedContent is set but Text is empty, the Summary array is empty (not nil).
func TestConvertAssistantContent_ReasoningEmptySummaryWhenNoText(t *testing.T) {
	content := []types.ContentPart{
		types.ReasoningContent{
			Text:             "",
			EncryptedContent: "enc-empty-text",
		},
	}

	items := convertAssistantContent(content, "openai", false)

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item := items[0].(ReasoningInputItem)
	if len(item.Summary) != 0 {
		t.Errorf("expected empty Summary when Text is empty, got %+v", item.Summary)
	}
}

// TestConvertAssistantContent_ReasoningInputItemSerializesCorrectly ensures the
// emitted ReasoningInputItem marshals to the JSON shape required by the API.
func TestConvertAssistantContent_ReasoningInputItemSerializesCorrectly(t *testing.T) {
	content := []types.ContentPart{
		types.ReasoningContent{
			Text:             "step one",
			EncryptedContent: "enc-abc",
		},
	}

	items := convertAssistantContent(content, "openai", false)
	if len(items) == 0 {
		t.Fatal("expected an item")
	}

	b, err := json.Marshal(items[0])
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if m["type"] != "reasoning" {
		t.Errorf(`type = %v, want "reasoning"`, m["type"])
	}
	if m["encrypted_content"] != "enc-abc" {
		t.Errorf(`encrypted_content = %v, want "enc-abc"`, m["encrypted_content"])
	}
}

func TestConvertToOpenResponsesInput_NonImageFileURLUsesInputFile(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.FileContent{
					FileData: types.FileData{
						Type:      types.FileDataTypeURL,
						URL:       "https://example.com/report.pdf",
						MediaType: "application/pdf",
					},
				},
			},
		},
	}

	input, _, _ := ConvertToOpenResponsesInput(msgs, "")
	items := input.([]interface{})
	msg := items[0].(MessageItem)
	parts := msg.Content.([]interface{})
	file := parts[0].(InputFileContent)
	if file.Type != "input_file" || file.FileURL != "https://example.com/report.pdf" {
		t.Fatalf("file part = %#v", file)
	}
}

func TestConvertToOpenResponsesInput_ToolResultContentFileParts(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "call_123",
					ToolName:   "read_files",
					Output: &types.ToolResultOutput{
						Type: types.ToolResultOutputContent,
						Content: []types.ToolResultContentBlock{
							types.TextContentBlock{Text: "attached files"},
							types.FileContentBlock{
								FileData: types.FileData{
									Type:      types.FileDataTypeURL,
									URL:       "https://example.com/report.pdf",
									MediaType: "application/pdf",
								},
							},
							types.FileContentBlock{
								FileData: types.FileData{
									Type:      types.FileDataTypeData,
									Data:      []byte("a,b\n1,2\n"),
									MediaType: "text/csv",
								},
								Filename: "data.csv",
							},
						},
					},
				},
			},
		},
	}

	input, _, warnings := ConvertToOpenResponsesInput(msgs, "")
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}

	items := input.([]interface{})
	output := items[0].(FunctionCallOutputItem)
	if output.CallID != "call_123" {
		t.Fatalf("CallID = %q, want call_123", output.CallID)
	}

	parts := output.Output.([]interface{})
	if text := parts[0].(InputTextContent); text.Type != "input_text" || text.Text != "attached files" {
		t.Fatalf("text part = %#v", text)
	}
	urlFile := parts[1].(InputFileContent)
	if urlFile.Type != "input_file" || urlFile.FileURL != "https://example.com/report.pdf" {
		t.Fatalf("url file part = %#v", urlFile)
	}
	dataFile := parts[2].(InputFileContent)
	if dataFile.Type != "input_file" || dataFile.Filename != "data.csv" || dataFile.FileData == "" {
		t.Fatalf("data file part = %#v", dataFile)
	}
}

func TestConvertToOpenResponsesInput_ReferenceFilePartsAreUnsupportedLikeTS(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.FileContent{
					MediaType: "application/pdf",
					FileData: types.FileData{
						Type: types.FileDataTypeReference,
						Reference: types.ProviderReference{
							"openai":         "file-openai",
							"open-responses": "file-openresponses",
						},
					},
				},
			},
		},
	}

	_, _, _, err := ConvertToOpenResponsesInputForProvider(msgs, "", "open-responses")
	if err == nil || !strings.Contains(err.Error(), "provider references are not supported") {
		t.Fatalf("error = %v, want unsupported provider reference", err)
	}
}

func TestConvertToOpenResponsesInput_TextFilePartsAreUnsupportedLikeTS(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.FileContent{
					FileData: types.FileData{
						Type: types.FileDataTypeText,
						Text: "inline text file",
					},
				},
			},
		},
	}

	_, _, _, err := ConvertToOpenResponsesInputForProvider(msgs, "", "open-responses")
	if err == nil || !strings.Contains(err.Error(), "text file parts are not supported") {
		t.Fatalf("error = %v, want unsupported text file", err)
	}
}

func TestConvertToOpenResponsesInput_ToolResultReferenceAndTextFilePartsWarnLikeTS(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "call_123",
					ToolName:   "read_files",
					Output: &types.ToolResultOutput{
						Type: types.ToolResultOutputContent,
						Content: []types.ToolResultContentBlock{
							types.FileContentBlock{
								MediaType: "application/pdf",
								FileData: types.FileData{
									Type: types.FileDataTypeReference,
									Reference: types.ProviderReference{
										"openai":         "file-openai",
										"open-responses": "file-openresponses",
									},
								},
							},
							types.FileContentBlock{
								MediaType: "text/plain",
								FileData: types.FileData{
									Type: types.FileDataTypeText,
									Text: "text-file",
								},
							},
						},
					},
				},
			},
		},
	}

	input, _, warnings, err := ConvertToOpenResponsesInputForProvider(msgs, "", "open-responses")
	if err != nil {
		t.Fatalf("ConvertToOpenResponsesInputForProvider() error = %v", err)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %#v, want 2 unsupported file-data warnings", warnings)
	}
	items := input.([]interface{})
	output := items[0].(FunctionCallOutputItem)
	parts := output.Output.([]interface{})
	if len(parts) != 0 {
		t.Fatalf("unsupported file blocks should be skipped, got %#v", parts)
	}
}

func TestConvertToOpenResponsesInput_ToolCallWithNamespaceFromProviderOptions(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ToolCallContent{
					ToolCallID: "call_123",
					ToolName:   "get_weather",
					Arguments:  map[string]interface{}{"city": "San Francisco"},
					ProviderOptions: map[string]interface{}{
						"openai": map[string]interface{}{"namespace": "weather"},
					},
				},
			},
		},
	}

	input, _, _, err := ConvertToOpenResponsesInputForProvider(msgs, "", "openai")
	if err != nil {
		t.Fatalf("ConvertToOpenResponsesInputForProvider() error = %v", err)
	}

	items := input.([]interface{})
	call := items[0].(FunctionCallItem)
	if call.Type != "function_call" {
		t.Fatalf("Type = %q, want function_call", call.Type)
	}
	if call.CallID != "call_123" || call.Name != "get_weather" {
		t.Fatalf("call = %#v", call)
	}
	if call.Arguments != `{"city":"San Francisco"}` {
		t.Fatalf("Arguments = %q", call.Arguments)
	}
	if call.Namespace != "weather" {
		t.Fatalf("Namespace = %q, want weather", call.Namespace)
	}
}

func TestConvertToOpenResponsesInput_ToolCallWithNamespaceFromProviderMetadata(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ToolCallContent{
					ToolCallID:       "call_456",
					ToolName:         "get_weather",
					Input:            `{"city":"Paris"}`,
					ProviderMetadata: json.RawMessage(`{"openai":{"namespace":"weather-metadata"}}`),
				},
			},
		},
	}

	input, _, _, err := ConvertToOpenResponsesInputForProvider(msgs, "", "openai")
	if err != nil {
		t.Fatalf("ConvertToOpenResponsesInputForProvider() error = %v", err)
	}

	items := input.([]interface{})
	call := items[0].(FunctionCallItem)
	if call.CallID != "call_456" || call.Arguments != `{"city":"Paris"}` {
		t.Fatalf("call = %#v", call)
	}
	if call.Namespace != "weather-metadata" {
		t.Fatalf("Namespace = %q, want weather-metadata", call.Namespace)
	}
}

func TestConvertToOpenResponsesInput_ClientAndProviderExecutedToolCalls(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ToolCallContent{
					ToolCallID:       "call_provider",
					ToolName:         "server_tool",
					Input:            `{"server":true}`,
					ProviderExecuted: true,
				},
				types.ToolCallContent{
					ToolCallID: "call_client",
					ToolName:   "client_tool",
					Input:      `{"client":true}`,
				},
			},
		},
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "call_client",
					ToolName:   "client_tool",
					Result:     "ok",
				},
			},
		},
	}

	input, _, _, err := ConvertToOpenResponsesInputForProvider(msgs, "", "openai")
	if err != nil {
		t.Fatalf("ConvertToOpenResponsesInputForProvider() error = %v", err)
	}

	items := input.([]interface{})
	if len(items) != 2 {
		t.Fatalf("items = %#v, want client function_call plus output", items)
	}
	call := items[0].(FunctionCallItem)
	if call.CallID != "call_client" || call.Name != "client_tool" {
		t.Fatalf("call = %#v", call)
	}
	output := items[1].(FunctionCallOutputItem)
	if output.CallID != "call_client" || output.Output != "ok" {
		t.Fatalf("output = %#v", output)
	}
}

// TestConvertToOpenResponsesInput_CustomToolCallReplay verifies that a
// tool-call content part whose ToolName resolves (via the declared Tools
// list) to a "provider" tool matching Config.CustomToolID replays as
// custom_tool_call, not function_call, mirroring TS convertToOpenResponsesInput's
// `customToolId != null && providerTool?.id === customToolId` branch. The
// wire `input` is the raw text (recovered from Arguments["input"], the Go
// analog of the already-parsed LanguageModelV4Prompt tool-call input TS
// checks with `typeof part.input === 'string'`), not a JSON-escaped string.
func TestConvertToOpenResponsesInput_CustomToolCallReplay(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ToolCallContent{
					ToolCallID: "call_1",
					ToolName:   "render",
					Arguments:  map[string]interface{}{"input": "<svg></svg>"},
					ProviderOptions: map[string]interface{}{
						"acme": map[string]interface{}{"itemId": "ct_item_1"},
					},
				},
			},
		},
	}
	tools := []types.Tool{{Type: "provider", ProviderID: "acme.custom", Name: "render"}}

	input, _, _, err := ConvertToOpenResponsesInputForProviderStrict(msgs, "", "acme", false, openResponsesExtensionOptions{
		Tools:        tools,
		CustomToolID: "acme.custom",
	})
	if err != nil {
		t.Fatalf("ConvertToOpenResponsesInputForProviderStrict() error = %v", err)
	}

	items := input.([]interface{})
	if len(items) != 1 {
		t.Fatalf("items = %#v, want 1 custom_tool_call item", items)
	}
	call, ok := items[0].(CustomToolCallItem)
	if !ok {
		t.Fatalf("items[0] = %#v (%T), want CustomToolCallItem", items[0], items[0])
	}
	if call.Type != "custom_tool_call" || call.CallID != "call_1" || call.Name != "render" || call.ID != "ct_item_1" {
		t.Fatalf("call = %#v", call)
	}
	if call.Input != "<svg></svg>" {
		t.Fatalf("Input = %q, want raw (un-escaped) text %q", call.Input, "<svg></svg>")
	}
}

// TestConvertToOpenResponsesInput_NonCustomToolCallStillUsesFunctionCall
// verifies that CustomToolID only affects tool calls whose declared tool's
// ProviderID actually matches -- an ordinary tool call still replays as
// function_call even when CustomToolID is configured, mirroring TS falling
// through to the `function_call` branch whenever `providerTool?.id !==
// customToolId`.
func TestConvertToOpenResponsesInput_NonCustomToolCallStillUsesFunctionCall(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ToolCallContent{
					ToolCallID: "call_2",
					ToolName:   "get_weather",
					Arguments:  map[string]interface{}{"city": "Paris"},
				},
			},
		},
	}
	tools := []types.Tool{
		{Type: "provider", ProviderID: "acme.custom", Name: "render"},
		{Name: "get_weather"},
	}

	input, _, _, err := ConvertToOpenResponsesInputForProviderStrict(msgs, "", "acme", false, openResponsesExtensionOptions{
		Tools:        tools,
		CustomToolID: "acme.custom",
	})
	if err != nil {
		t.Fatalf("ConvertToOpenResponsesInputForProviderStrict() error = %v", err)
	}

	items := input.([]interface{})
	call, ok := items[0].(FunctionCallItem)
	if !ok {
		t.Fatalf("items[0] = %#v (%T), want FunctionCallItem", items[0], items[0])
	}
	if call.CallID != "call_2" || call.Name != "get_weather" {
		t.Fatalf("call = %#v", call)
	}
}

// TestConvertToOpenResponsesInput_CustomToolResultsPreserveTextImagesAndFiles
// ports the TS open-responses test "preserves text, images, and files in
// custom tool history" (convert-to-open-responses-input.test.ts, describe
// "custom tool results"): a tool-result whose ToolName resolves to
// Config.CustomToolID replays as custom_tool_call_output (not
// function_call_output); the output content itself is computed identically
// either way.
func TestConvertToOpenResponsesInput_CustomToolResultsPreserveTextImagesAndFiles(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "call_1",
					ToolName:   "render",
					Output: &types.ToolResultOutput{
						Type: types.ToolResultOutputContent,
						Content: []types.ToolResultContentBlock{
							types.TextContentBlock{Text: "Rendered result"},
							types.FileContentBlock{
								MediaType: "image/png",
								FileData: types.FileData{
									Type: types.FileDataTypeURL,
									URL:  "https://example.com/result.png",
								},
							},
							types.FileContentBlock{
								MediaType: "application/pdf",
								FileData: types.FileData{
									Type: types.FileDataTypeURL,
									URL:  "https://example.com/result.pdf",
								},
							},
							types.FileContentBlock{
								MediaType: "image/png",
								FileData: types.FileData{
									Type: types.FileDataTypeData,
									Data: []byte("image"),
								},
							},
							types.FileContentBlock{
								MediaType: "application/pdf",
								Filename:  "result.pdf",
								FileData: types.FileData{
									Type: types.FileDataTypeData,
									Data: []byte("pdf"),
								},
							},
						},
					},
				},
			},
		},
	}
	tools := []types.Tool{{Type: "provider", ProviderID: "acme.custom", Name: "render"}}

	input, _, warnings, err := ConvertToOpenResponsesInputForProviderStrict(msgs, "", "acme", false, openResponsesExtensionOptions{
		Tools:        tools,
		CustomToolID: "acme.custom",
	})
	if err != nil {
		t.Fatalf("ConvertToOpenResponsesInputForProviderStrict() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}

	items := input.([]interface{})
	if len(items) != 1 {
		t.Fatalf("items = %#v, want 1 custom_tool_call_output item", items)
	}
	output, ok := items[0].(CustomToolCallOutputItem)
	if !ok {
		t.Fatalf("items[0] = %#v (%T), want CustomToolCallOutputItem", items[0], items[0])
	}
	if output.Type != "custom_tool_call_output" || output.CallID != "call_1" {
		t.Fatalf("output = %#v", output)
	}

	parts := output.Output.([]interface{})
	if len(parts) != 5 {
		t.Fatalf("parts = %#v, want 5", parts)
	}
	if text := parts[0].(InputTextContent); text.Type != "input_text" || text.Text != "Rendered result" {
		t.Fatalf("text part = %#v", text)
	}
	if img := parts[1].(InputImageContent); img.Type != "input_image" || img.ImageURL != "https://example.com/result.png" {
		t.Fatalf("url image part = %#v", img)
	}
	if file := parts[2].(InputFileContent); file.Type != "input_file" || file.FileURL != "https://example.com/result.pdf" {
		t.Fatalf("url file part = %#v", file)
	}
	if img := parts[3].(InputImageContent); img.Type != "input_image" || img.ImageURL != "data:image/png;base64,aW1hZ2U=" {
		t.Fatalf("data image part = %#v", img)
	}
	if file := parts[4].(InputFileContent); file.Type != "input_file" || file.Filename != "result.pdf" || file.FileData != "data:application/pdf;base64,cGRm" {
		t.Fatalf("data file part = %#v", file)
	}
}
