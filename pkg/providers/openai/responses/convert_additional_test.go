package responses

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestConvertPromptToInput_UserAndAssistantVariants(t *testing.T) {
	t.Parallel()

	img := []byte{0x89, 0x50, 0x4E, 0x47}
	prompt := types.Prompt{
		System: "system-instructions",
		Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: "hi"},
					types.ImageContent{
						Image:           img,
						MimeType:        "image/png",
						ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"detail": "high"}},
					},
					types.FileContent{URL: "https://example.com/doc.pdf", MediaType: "application/pdf"},
					types.FileContent{Reference: "file_abc"},
				},
			},
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.TextContent{Text: "assistant-text"},
				},
				ToolCalls: []types.ToolCall{
					{
						ID:        "tc1",
						ToolName:  "weather",
						Arguments: map[string]interface{}{"city": "SF"},
						ProviderMetadata: map[string]interface{}{
							"openai": map[string]interface{}{"namespace": "ns1"},
						},
					},
				},
			},
		},
	}

	input := ConvertPromptToInput(prompt, "developer")
	if len(input) < 3 {
		t.Fatalf("expected at least 3 input items, got %d", len(input))
	}

	sys, ok := input[0].(SystemMessage)
	if !ok || sys.Role != "developer" || sys.Content != "system-instructions" {
		t.Fatalf("system message conversion mismatch: %#v", input[0])
	}
	user, ok := input[1].(UserMessage)
	if !ok {
		t.Fatalf("expected UserMessage at input[1], got %T", input[1])
	}
	parts, ok := user.Content.([]interface{})
	if !ok || len(parts) < 3 {
		t.Fatalf("expected multi-part user content, got %#v", user.Content)
	}
	imagePart := parts[1].(UserImageURLPart)
	if imagePart.Detail != "high" || !strings.HasPrefix(imagePart.ImageURL, "data:image/png;base64,") {
		t.Fatalf("image part mismatch: %+v", imagePart)
	}
	if wantPrefix := base64.StdEncoding.EncodeToString(img); !strings.Contains(imagePart.ImageURL, wantPrefix) {
		t.Fatalf("expected base64 payload in image URL: %s", imagePart.ImageURL)
	}

	toolItem, ok := input[len(input)-1].(FunctionCallItem)
	if !ok || toolItem.Namespace != "ns1" || toolItem.Name != "weather" {
		t.Fatalf("assistant tool call conversion mismatch: %#v", input[len(input)-1])
	}
}

func TestConvertPromptToInputWithOptionsUnsupportedFileDefaultAndPassThrough(t *testing.T) {
	prompt := types.Prompt{Messages: []types.Message{{
		Role: types.RoleUser,
		Content: []types.ContentPart{
			types.FileContent{Data: []byte("csv"), MediaType: "text/csv", Filename: "data.csv"},
		},
	}}}
	if _, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{}); err == nil {
		t.Fatal("expected unsupported file media type error")
	}
	if _, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{ProviderOptionsName: "azure"}); err == nil || !strings.Contains(err.Error(), "providerOptions.azure.passThroughUnsupportedFiles") {
		t.Fatalf("azure unsupported file error = %v, want azure providerOptions hint", err)
	}
	input, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{PassThroughUnsupportedFiles: true})
	if err != nil {
		t.Fatalf("ConvertPromptToInputWithOptions error = %v", err)
	}
	user := input[0].(UserMessage)
	parts := user.Content.([]interface{})
	file := parts[0].(map[string]interface{})
	if file["type"] != "input_file" || file["filename"] != "data.csv" {
		t.Fatalf("file part = %#v", file)
	}
}

func TestConvertPromptToInput_UserContentArrayAndDefaultFilenames(t *testing.T) {
	t.Parallel()

	input, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: "hello"},
				},
			},
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{Data: []byte("pdf"), MediaType: "application/pdf"},
				},
			},
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{
						Reference: "file_img",
						MediaType: "image/png",
						ProviderOptions: map[string]interface{}{
							"openai": map[string]interface{}{"imageDetail": "low"},
						},
					},
				},
			},
		},
	}, "system", ConvertOptions{})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	textParts := input[0].(UserMessage).Content.([]interface{})
	text, ok := textParts[0].(UserTextPart)
	if !ok || text.Type != "input_text" || text.Text != "hello" {
		t.Fatalf("text part = %#v, want input_text hello", textParts[0])
	}
	fileParts := input[1].(UserMessage).Content.([]interface{})
	file, ok := fileParts[0].(map[string]interface{})
	if !ok || file["type"] != "input_file" || file["filename"] != "part-0.pdf" {
		t.Fatalf("file part = %#v, want default PDF filename", fileParts[0])
	}
	imageParts := input[2].(UserMessage).Content.([]interface{})
	image, ok := imageParts[0].(map[string]interface{})
	if !ok || image["type"] != "input_image" || image["file_id"] != "file_img" || image["detail"] != "low" {
		t.Fatalf("image reference part = %#v, want input_image file_id", imageParts[0])
	}
}

func TestConvertPromptToInput_UserFileDataVariants(t *testing.T) {
	t.Parallel()

	input, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{
						FileData: types.FileData{
							Type:      types.FileDataTypeURL,
							URL:       "https://example.com/doc.pdf",
							MediaType: "application/pdf",
						},
					},
					types.FileContent{
						FileData: types.FileData{
							Type:      types.FileDataTypeData,
							Data:      []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
							MediaType: "image/*",
						},
					},
					types.FileContent{
						FileData: types.FileData{
							Type:      types.FileDataTypeReference,
							Reference: types.ProviderReference{"openai": "file_openai"},
							MediaType: "application/pdf",
						},
					},
					types.FileContent{
						FileData: types.FileData{
							Type:       types.FileDataTypeData,
							DataString: "file-compat-image",
							MediaType:  "image/png",
						},
					},
					types.FileContent{
						FileData: types.FileData{
							Type:       types.FileDataTypeData,
							DataString: "file-compat-pdf",
							MediaType:  "application/pdf",
						},
					},
				},
			},
		},
	}, "system", ConvertOptions{FileIDPrefixes: []string{"file-"}})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	parts := input[0].(UserMessage).Content.([]interface{})
	fileURL, ok := parts[0].(UserFilePart)
	if !ok || fileURL.FileURL != "https://example.com/doc.pdf" {
		t.Fatalf("parts[0] = %#v, want input_file file_url", parts[0])
	}
	image, ok := parts[1].(UserImageURLPart)
	if !ok || image.Type != "input_image" || !strings.HasPrefix(image.ImageURL, "data:image/png;base64,") {
		t.Fatalf("parts[1] = %#v, want detected image/png data URL", parts[1])
	}
	reference, ok := parts[2].(map[string]interface{})
	if !ok || reference["type"] != "input_file" || reference["file_id"] != "file_openai" {
		t.Fatalf("parts[2] = %#v, want input_file file_id", parts[2])
	}
	imageFileID, ok := parts[3].(map[string]interface{})
	if !ok || imageFileID["type"] != "input_image" || imageFileID["file_id"] != "file-compat-image" {
		t.Fatalf("parts[3] = %#v, want input_image file_id", parts[3])
	}
	pdfFileID, ok := parts[4].(map[string]interface{})
	if !ok || pdfFileID["type"] != "input_file" || pdfFileID["file_id"] != "file-compat-pdf" {
		t.Fatalf("parts[4] = %#v, want input_file file_id", parts[4])
	}

	_, err = ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{types.FileContent{
				FileData: types.FileData{Type: types.FileDataTypeText, Text: "inline text"},
			}},
		}},
	}, "system", ConvertOptions{})
	if err == nil {
		t.Fatal("expected text file parts to be rejected")
	}
}

func TestConvertPromptToInput_ProviderOptionsName(t *testing.T) {
	t.Parallel()

	input, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{types.FileContent{
				FileData: types.FileData{
					Type: types.FileDataTypeReference,
					Reference: types.ProviderReference{
						"openai": "file_openai",
						"azure":  "file_azure",
					},
					MediaType: "image/png",
				},
				ProviderOptions: map[string]interface{}{
					"azure": map[string]interface{}{"imageDetail": "low"},
				},
			}},
		}},
	}, "system", ConvertOptions{ProviderOptionsName: "azure"})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	parts := input[0].(UserMessage).Content.([]interface{})
	image, ok := parts[0].(map[string]interface{})
	if !ok || image["type"] != "input_image" || image["file_id"] != "file_azure" || image["detail"] != "low" {
		t.Fatalf("provider-specific image reference = %#v, want azure file_id and detail", parts[0])
	}
}

func TestConvertPromptToInputResponsesReasoningItems(t *testing.T) {
	reasoning := types.ReasoningContent{
		Text:             "summary",
		EncryptedContent: "enc_123",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"itemId": "rs_123"},
		},
	}
	prompt := types.Prompt{Messages: []types.Message{{
		Role:    types.RoleAssistant,
		Content: []types.ContentPart{reasoning},
	}}}

	input, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{Store: true})
	if err != nil {
		t.Fatalf("ConvertPromptToInputWithOptions: %v", err)
	}
	itemRef := input[0].(map[string]interface{})
	if itemRef["type"] != "item_reference" || itemRef["id"] != "rs_123" {
		t.Fatalf("stored reasoning item = %#v, want item_reference rs_123", itemRef)
	}

	input, err = ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{HasPreviousResponseID: true, Store: true})
	if err != nil {
		t.Fatalf("ConvertPromptToInputWithOptions with previous response id: %v", err)
	}
	if len(input) != 0 {
		t.Fatalf("stored reasoning with previous response id should be skipped, got %#v", input)
	}

	reasoning.ProviderOptions = nil
	prompt.Messages[0].Content = []types.ContentPart{reasoning}
	input, err = ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertPromptToInputWithOptions full reasoning: %v", err)
	}
	full := input[0].(map[string]interface{})
	if full["type"] != "reasoning" || full["encrypted_content"] != "enc_123" {
		t.Fatalf("full reasoning item = %#v", full)
	}
	summary := full["summary"].([]map[string]interface{})
	if len(summary) != 1 || summary[0]["type"] != "summary_text" || summary[0]["text"] != "summary" {
		t.Fatalf("reasoning summary = %#v", summary)
	}
}

func TestConvertPromptToInputResponsesReasoningConversationSkipAndDedup(t *testing.T) {
	reasoning := types.ReasoningContent{
		Text:             "summary one",
		EncryptedContent: "enc_1",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"itemId": "rs_123"},
		},
	}
	duplicate := reasoning
	duplicate.Text = "summary two"
	duplicate.EncryptedContent = "enc_2"
	prompt := types.Prompt{Messages: []types.Message{{
		Role:    types.RoleAssistant,
		Content: []types.ContentPart{reasoning, duplicate},
	}}}

	input, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{HasConversation: true})
	if err != nil {
		t.Fatalf("ConvertPromptToInputWithOptions with conversation: %v", err)
	}
	if len(input) != 0 {
		t.Fatalf("stored reasoning with conversation should be skipped, got %#v", input)
	}

	input, err = ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertPromptToInputWithOptions dedupe: %v", err)
	}
	if len(input) != 1 {
		t.Fatalf("duplicate reasoning item IDs should produce one item, got %#v", input)
	}
	item := input[0].(map[string]interface{})
	if item["id"] != "rs_123" || item["encrypted_content"] != "enc_2" {
		t.Fatalf("merged reasoning item = %#v", item)
	}
	summary := item["summary"].([]map[string]interface{})
	if len(summary) != 2 || summary[0]["text"] != "summary one" || summary[1]["text"] != "summary two" {
		t.Fatalf("merged reasoning summary = %#v", summary)
	}

	summaryOnly := types.Prompt{Messages: []types.Message{{
		Role: types.RoleAssistant,
		Content: []types.ContentPart{
			types.ReasoningContent{Text: "summary without encrypted content"},
		},
	}}}
	input, err = ConvertPromptToInputWithOptions(summaryOnly, "system", ConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertPromptToInputWithOptions summary-only reasoning: %v", err)
	}
	if len(input) != 0 {
		t.Fatalf("summary-only reasoning should be stripped when store=false, got %#v", input)
	}

	firstWithoutEncrypted := reasoning
	firstWithoutEncrypted.EncryptedContent = ""
	prompt.Messages[0].Content = []types.ContentPart{firstWithoutEncrypted, duplicate}
	input, err = ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertPromptToInputWithOptions late encrypted reasoning: %v", err)
	}
	if len(input) != 1 {
		t.Fatalf("late encrypted duplicate reasoning should produce one item, got %#v", input)
	}
	item = input[0].(map[string]interface{})
	if item["encrypted_content"] != "enc_2" {
		t.Fatalf("late encrypted reasoning item = %#v", item)
	}
	summary = item["summary"].([]map[string]interface{})
	if len(summary) != 2 || summary[0]["text"] != "summary one" || summary[1]["text"] != "summary two" {
		t.Fatalf("late encrypted reasoning summary = %#v", summary)
	}
}

func TestConvertPromptToInputResponsesStoredAssistantTextAndClientFunctionCalls(t *testing.T) {
	prompt := types.Prompt{Messages: []types.Message{{
		Role: types.RoleAssistant,
		Content: []types.ContentPart{
			types.TextContent{
				Text: "stored answer",
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{"itemId": "msg_123", "phase": "final_answer"},
				},
			},
		},
		ToolCalls: []types.ToolCall{
			{
				ID:        "call_1",
				ToolName:  "lookup",
				Arguments: map[string]interface{}{"q": "go"},
				ProviderMetadata: map[string]interface{}{
					"openai": map[string]interface{}{"itemId": "fc_123"},
				},
			},
		},
	}}}

	input, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{Store: true})
	if err != nil {
		t.Fatalf("ConvertPromptToInputWithOptions store=true: %v", err)
	}
	if len(input) != 2 {
		t.Fatalf("input length = %d, want text reference plus full client function call: %#v", len(input), input)
	}
	textRef := input[0].(map[string]interface{})
	callItem := input[1].(FunctionCallItem)
	if textRef["type"] != "item_reference" || textRef["id"] != "msg_123" {
		t.Fatalf("text item = %#v, want item_reference msg_123", textRef)
	}
	if callItem.ID != "" || callItem.CallID != "call_1" || callItem.Name != "lookup" {
		t.Fatalf("function call item = %#v, want full client function_call without item id", callItem)
	}

	input, err = ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{HasConversation: true, Store: true})
	if err != nil {
		t.Fatalf("ConvertPromptToInputWithOptions conversation: %v", err)
	}
	if len(input) != 0 {
		t.Fatalf("stored assistant text/function calls should be skipped with conversation, got %#v", input)
	}

	input, err = ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertPromptToInputWithOptions full items: %v", err)
	}
	msg := input[0].(AssistantMessageItem)
	if msg.ID != "msg_123" || msg.Phase == nil || *msg.Phase != "final_answer" {
		t.Fatalf("assistant message metadata = %#v, want id and phase", msg)
	}
	call := input[1].(FunctionCallItem)
	if call.ID != "" {
		t.Fatalf("function call ID = %q, want empty for client-executed call", call.ID)
	}
}

func TestConvertToolResultOutput_StructuredContentAndFallbacks(t *testing.T) {
	t.Parallel()

	out := toolResultOutput(types.ToolResultContent{
		ToolCallID: "tc1",
		Output: &types.ToolResultOutput{
			Type: types.ToolResultOutputContent,
			Content: []types.ToolResultContentBlock{
				types.TextContentBlock{Text: "hello"},
				types.ImageContentBlock{
					MediaType:       "image/png",
					Data:            []byte{0x01, 0x02},
					ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"imageDetail": "low"}},
				},
				types.FileContentBlock{
					MediaType: "application/json",
					Data:      []byte(`{"a":1}`),
					Filename:  "x.json",
				},
				types.FileContentBlock{
					MediaType:       "image/png",
					URL:             "https://example.com/result.png",
					ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"imageDetail": "high"}},
				},
				types.FileContentBlock{
					MediaType:       "image/png",
					Data:            []byte{0x03, 0x04},
					ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"imageDetail": "auto"}},
				},
				types.FileContentBlock{
					MediaType: "application/pdf",
					Reference: "file_unsupported",
				},
				types.FileContentBlock{
					Text: "unsupported text file",
				},
			},
		},
	})
	parts, ok := out.([]CustomToolCallOutputPart)
	if !ok || len(parts) != 5 {
		t.Fatalf("expected 5 structured parts, got %#v", out)
	}
	if parts[1].Detail != "low" || parts[2].Filename != "x.json" {
		t.Fatalf("structured part fields mismatch: %#v", parts)
	}
	if parts[3].Type != "input_image" || parts[3].ImageURL != "https://example.com/result.png" || parts[3].Detail != "high" {
		t.Fatalf("image URL file block mismatch: %#v", parts[3])
	}
	if parts[4].Type != "input_image" || !strings.HasPrefix(parts[4].ImageURL, "data:image/png;base64,") || parts[4].Detail != "auto" {
		t.Fatalf("image data file block mismatch: %#v", parts[4])
	}

	deny := toolResultOutput(types.ToolResultContent{
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: "blocked"},
	})
	if deny != "blocked" {
		t.Fatalf("execution denied output = %#v", deny)
	}

	plain := toolResultOutput(types.ToolResultContent{Result: 42})
	if plain != "42" {
		t.Fatalf("fallback formatting mismatch: %#v", plain)
	}
}

func TestConvertToolResultOutput_ProviderOptionsName(t *testing.T) {
	t.Parallel()

	out := toolResultOutputWithOptions(types.ToolResultContent{
		Output: &types.ToolResultOutput{
			Type: types.ToolResultOutputContent,
			Content: []types.ToolResultContentBlock{
				types.ImageContentBlock{
					MediaType: "image/png",
					Data:      []byte{0x01, 0x02},
					ProviderOptions: map[string]interface{}{
						"azure": map[string]interface{}{"imageDetail": "high"},
					},
				},
				types.FileContentBlock{
					MediaType: "image/png",
					URL:       "https://example.com/image.png",
					ProviderOptions: map[string]interface{}{
						"azure": map[string]interface{}{"imageDetail": "low"},
					},
				},
			},
		},
	}, ConvertOptions{ProviderOptionsName: "azure"})
	parts, ok := out.([]CustomToolCallOutputPart)
	if !ok || len(parts) != 2 {
		t.Fatalf("expected 2 structured parts, got %#v", out)
	}
	if parts[0].Detail != "high" || parts[1].Detail != "low" {
		t.Fatalf("provider-specific image detail mismatch: %#v", parts)
	}
}

func TestConvertPromptToInput_ToolApprovalResponses(t *testing.T) {
	t.Parallel()

	input := ConvertPromptToInput(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolApprovalResponseContent{
						ApprovalID: "approval-1",
						Approved:   true,
					},
					types.ToolApprovalResponseContent{
						ApprovalID: "approval-1",
						Approved:   false,
					},
					types.ToolResultContent{
						ToolCallID: "call-1",
						Output:     &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "ok"},
					},
				},
			},
		},
	}, "system")

	if len(input) != 2 {
		t.Fatalf("input len = %d, want 2: %#v", len(input), input)
	}
	approval, ok := input[0].(MCPApprovalResponse)
	if !ok {
		t.Fatalf("input[0] = %T, want MCPApprovalResponse", input[0])
	}
	if approval.Type != "mcp_approval_response" || approval.ApprovalRequestID != "approval-1" || !approval.Approve {
		t.Fatalf("approval item = %+v, want approved mcp_approval_response", approval)
	}
	result, ok := input[1].(FunctionCallOutputItem)
	if !ok {
		t.Fatalf("input[1] = %T, want FunctionCallOutputItem", input[1])
	}
	if result.CallID != "call-1" || result.Output != "ok" {
		t.Fatalf("result item = %+v, want call-1 ok", result)
	}
}

func TestConvertPromptToInput_SkipsApprovalDeniedToolOutput(t *testing.T) {
	t.Parallel()

	input := ConvertPromptToInput(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolApprovalResponseContent{
						ApprovalID: "approval-1",
						Approved:   false,
					},
					types.ToolResultContent{
						ToolCallID: "call-1",
						Output: &types.ToolResultOutput{
							Type:   types.ToolResultOutputExecutionDenied,
							Reason: "blocked",
						},
						ProviderOptions: map[string]interface{}{
							"openai": map[string]interface{}{"approvalId": "approval-1"},
						},
					},
				},
			},
		},
	}, "system")

	if len(input) != 1 {
		t.Fatalf("input len = %d, want only approval response: %#v", len(input), input)
	}
	approval, ok := input[0].(MCPApprovalResponse)
	if !ok {
		t.Fatalf("input[0] = %T, want MCPApprovalResponse", input[0])
	}
	if approval.ApprovalRequestID != "approval-1" || approval.Approve {
		t.Fatalf("approval item = %+v, want denied approval-1", approval)
	}
}

func TestConvertPromptToInput_StoredToolApprovalAddsItemReference(t *testing.T) {
	t.Parallel()

	input, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolApprovalResponseContent{
						ApprovalID: "approval-1",
						Approved:   true,
					},
				},
			},
		},
	}, "system", ConvertOptions{Store: true})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if len(input) != 2 {
		t.Fatalf("input len = %d, want item_reference + approval: %#v", len(input), input)
	}
	ref, ok := input[0].(map[string]interface{})
	if !ok || ref["type"] != "item_reference" || ref["id"] != "approval-1" {
		t.Fatalf("input[0] = %#v, want item_reference approval-1", input[0])
	}
	approval, ok := input[1].(MCPApprovalResponse)
	if !ok || approval.ApprovalRequestID != "approval-1" || !approval.Approve {
		t.Fatalf("input[1] = %#v, want approved mcp_approval_response", input[1])
	}
}

func TestConvertPromptToInput_OpenAISpecialToolOutputs(t *testing.T) {
	t.Parallel()

	patchOutput := "applied"
	input, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "search-call",
						ToolName:   "openai.tool_search",
						Output: &types.ToolResultOutput{
							Type: types.ToolResultOutputJSON,
							Value: map[string]interface{}{
								"tools": []interface{}{
									map[string]interface{}{"type": "function", "name": "weather"},
								},
							},
						},
					},
					types.ToolResultContent{
						ToolCallID: "local-call",
						ToolName:   "openai.local_shell",
						Output: &types.ToolResultOutput{
							Type:  types.ToolResultOutputJSON,
							Value: map[string]interface{}{"output": "stdout"},
						},
					},
					types.ToolResultContent{
						ToolCallID: "shell-call",
						ToolName:   "openai.shell",
						Output: &types.ToolResultOutput{
							Type: types.ToolResultOutputJSON,
							Value: map[string]interface{}{
								"output": []interface{}{
									map[string]interface{}{
										"stdout": "ok",
										"stderr": "",
										"outcome": map[string]interface{}{
											"type":     "exit",
											"exitCode": 0,
										},
									},
								},
							},
						},
					},
					types.ToolResultContent{
						ToolCallID: "patch-call",
						ToolName:   "apply_patch",
						Output: &types.ToolResultOutput{
							Type: types.ToolResultOutputJSON,
							Value: map[string]interface{}{
								"status": "completed",
								"output": patchOutput,
							},
						},
					},
				},
			},
		},
	}, "system", ConvertOptions{
		HasLocalShellTool: true,
		HasShellTool:      true,
		HasApplyPatchTool: true,
	})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if len(input) != 4 {
		t.Fatalf("input len = %d, want 4 special tool outputs: %#v", len(input), input)
	}
	search, ok := input[0].(ToolSearchOutputItem)
	if !ok || search.Type != "tool_search_output" || search.Execution != "client" || search.CallID == nil || *search.CallID != "search-call" || len(search.Tools) != 1 {
		t.Fatalf("input[0] = %#v, want tool_search_output", input[0])
	}
	local, ok := input[1].(LocalShellCallOutput)
	if !ok || local.Type != "local_shell_call_output" || local.CallID != "local-call" || local.Output != "stdout" {
		t.Fatalf("input[1] = %#v, want local_shell_call_output", input[1])
	}
	shell, ok := input[2].(ShellCallOutput)
	if !ok || shell.Type != "shell_call_output" || shell.CallID != "shell-call" || len(shell.Output) != 1 {
		t.Fatalf("input[2] = %#v, want shell_call_output", input[2])
	}
	if shell.Output[0].Outcome.Type != "exit" || shell.Output[0].Outcome.ExitCode == nil || *shell.Output[0].Outcome.ExitCode != 0 {
		t.Fatalf("shell outcome = %#v, want exit 0", shell.Output[0].Outcome)
	}
	patch, ok := input[3].(ApplyPatchCallOutput)
	if !ok || patch.Type != "apply_patch_call_output" || patch.CallID != "patch-call" || patch.Status != "completed" || patch.Output == nil || *patch.Output != patchOutput {
		t.Fatalf("input[3] = %#v, want apply_patch_call_output", input[3])
	}
}

func TestConvertPromptToInput_OpenAICustomToolOutput(t *testing.T) {
	t.Parallel()

	input, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "custom-call",
						ToolName:   "openai.grammar_tool",
						Output: &types.ToolResultOutput{
							Type:  types.ToolResultOutputJSON,
							Value: map[string]interface{}{"answer": "ok"},
						},
					},
				},
			},
		},
	}, "system", ConvertOptions{CustomToolNames: map[string]bool{"grammar_tool": true}})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if len(input) != 1 {
		t.Fatalf("input len = %d, want custom tool output: %#v", len(input), input)
	}
	custom, ok := input[0].(CustomToolCallOutput)
	if !ok || custom.Type != "custom_tool_call_output" || custom.CallID != "custom-call" || custom.Output != `{"answer":"ok"}` {
		t.Fatalf("input[0] = %#v, want custom_tool_call_output", input[0])
	}
}

func TestConvertPromptToInput_OpenAISpecialAssistantToolCalls(t *testing.T) {
	t.Parallel()

	input, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				ToolCalls: []types.ToolCall{
					{
						ID:        "search-call",
						ToolName:  "openai.tool_search",
						Arguments: map[string]interface{}{"call_id": "client-call", "arguments": map[string]interface{}{"query": "docs"}},
						ProviderMetadata: map[string]interface{}{
							"openai": map[string]interface{}{"itemId": "search-item"},
						},
					},
					{
						ID:       "shell-call",
						ToolName: "openai.shell",
						Arguments: map[string]interface{}{"action": map[string]interface{}{
							"commands":        []interface{}{"echo ok"},
							"maxOutputLength": float64(2000),
						}},
					},
					{
						ID:           "custom-call",
						ToolName:     "grammar_tool",
						RawArguments: "raw input",
					},
				},
			},
		},
	}, "system", ConvertOptions{
		HasShellTool:    true,
		CustomToolNames: map[string]bool{"grammar_tool": true},
	})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if len(input) != 3 {
		t.Fatalf("input len = %d, want three assistant tool-call items: %#v", len(input), input)
	}
	search, ok := input[0].(ToolSearchCallItem)
	if !ok || search.Type != "tool_search_call" || search.ID != "search-item" || search.Execution != "client" || search.CallID == nil || *search.CallID != "client-call" {
		t.Fatalf("input[0] = %#v, want client tool_search_call", input[0])
	}
	shell, ok := input[1].(ShellCall)
	if !ok || shell.Type != "shell_call" || shell.CallID != "shell-call" || len(shell.Action.Commands) != 1 || shell.Action.Commands[0] != "echo ok" {
		t.Fatalf("input[1] = %#v, want shell_call", input[1])
	}
	custom, ok := input[2].(CustomToolCallItem)
	if !ok || custom.Type != "custom_tool_call" || custom.Name != "grammar_tool" || custom.Input != "raw input" {
		t.Fatalf("input[2] = %#v, want custom_tool_call", input[2])
	}
}

func TestConvertPromptToInput_OpenAIAssistantToolResultOutputs(t *testing.T) {
	t.Parallel()

	input, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "server-search-call",
						ToolName:   "openai.tool_search",
						Output: &types.ToolResultOutput{
							Type: types.ToolResultOutputJSON,
							Value: map[string]interface{}{
								"tools": []interface{}{map[string]interface{}{"type": "function", "name": "weather"}},
							},
						},
						ProviderOptions: map[string]interface{}{
							"openai": map[string]interface{}{"itemId": "search-output-item"},
						},
					},
					types.ToolResultContent{
						ToolCallID: "shell-call",
						ToolName:   "openai.shell",
						Output: &types.ToolResultOutput{
							Type: types.ToolResultOutputJSON,
							Value: map[string]interface{}{
								"output": []interface{}{map[string]interface{}{
									"stdout":  "ok",
									"stderr":  "",
									"outcome": map[string]interface{}{"type": "timeout"},
								}},
							},
						},
					},
					types.ToolResultContent{
						ToolCallID: "denied-call",
						ToolName:   "provider_tool",
						Output: &types.ToolResultOutput{
							Type:  types.ToolResultOutputJSON,
							Value: map[string]interface{}{"type": "execution-denied"},
						},
					},
				},
			},
		},
	}, "system", ConvertOptions{HasShellTool: true})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if len(input) != 2 {
		t.Fatalf("input len = %d, want two assistant tool-result outputs: %#v", len(input), input)
	}
	search, ok := input[0].(ToolSearchOutputItem)
	if !ok || search.Type != "tool_search_output" || search.ID != "search-output-item" || search.Execution != "server" || search.CallID != nil {
		t.Fatalf("input[0] = %#v, want server tool_search_output", input[0])
	}
	shell, ok := input[1].(ShellCallOutput)
	if !ok || shell.Type != "shell_call_output" || shell.CallID != "shell-call" || len(shell.Output) != 1 || shell.Output[0].Outcome.Type != "timeout" {
		t.Fatalf("input[1] = %#v, want shell_call_output timeout", input[1])
	}
}

func TestConvertPromptToInput_AssistantToolCallContentParts(t *testing.T) {
	t.Parallel()

	input, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ToolCallContent{
						ToolCallID: "call-1",
						ToolName:   "weather",
						Arguments:  map[string]interface{}{"city": "SF"},
						ProviderOptions: map[string]interface{}{
							"openai": map[string]interface{}{"itemId": "fc_123", "namespace": "ns1"},
						},
					},
					types.ToolCallContent{
						ToolCallID: "custom-call",
						ToolName:   "grammar_tool",
						Input:      "raw input",
					},
					types.ToolCallContent{
						ToolCallID: "raw-call",
						ToolName:   "write_sql",
						Input:      "SELECT 1",
					},
				},
			},
		},
	}, "system", ConvertOptions{CustomToolNames: map[string]bool{"grammar_tool": true}})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if len(input) != 3 {
		t.Fatalf("input len = %d, want three tool-call content items: %#v", len(input), input)
	}
	call, ok := input[0].(FunctionCallItem)
	if !ok || call.ID != "" || call.Namespace != "ns1" || call.Arguments != `{"city":"SF"}` {
		t.Fatalf("input[0] = %#v, want client function_call without item id and with namespace", input[0])
	}
	custom, ok := input[1].(CustomToolCallItem)
	if !ok || custom.Type != "custom_tool_call" || custom.Input != "raw input" {
		t.Fatalf("input[1] = %#v, want custom_tool_call from content part", input[1])
	}
	raw, ok := input[2].(FunctionCallItem)
	if !ok || raw.Name != "write_sql" || raw.Arguments != `"SELECT 1"` {
		t.Fatalf("input[2] = %#v, want function_call with JSON-string arguments", input[2])
	}
}

func TestConvertPromptToInput_AssistantToolCallContentDeduplicatesTopLevelToolCalls(t *testing.T) {
	t.Parallel()

	input, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ToolCallContent{
						ToolCallID: "call-1",
						ToolName:   "weather",
						Arguments:  map[string]interface{}{"city": "SF"},
						ProviderOptions: map[string]interface{}{
							"openai": map[string]interface{}{"itemId": "fc_123"},
						},
					},
				},
				ToolCalls: []types.ToolCall{{
					ID:               "call-1",
					ToolName:         "weather",
					Arguments:        map[string]interface{}{"city": "SF"},
					ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"itemId": "fc_123"}},
				}},
			},
		},
	}, "system", ConvertOptions{PassThroughUnsupportedFiles: true})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if len(input) != 1 {
		t.Fatalf("input len = %d, want one deduplicated function_call: %#v", len(input), input)
	}
	if call, ok := input[0].(FunctionCallItem); !ok || call.ID != "" || call.CallID != "call-1" {
		t.Fatalf("input[0] = %#v, want client function_call from content part without item id", input[0])
	}
}

func TestConvertPromptToInput_ClientExecutedToolCallsDoNotUseItemIDs(t *testing.T) {
	t.Parallel()

	for _, store := range []bool{false, true} {
		t.Run(fmt.Sprintf("store=%v", store), func(t *testing.T) {
			t.Parallel()

			input, err := ConvertPromptToInputWithOptions(types.Prompt{
				Messages: []types.Message{
					{
						Role: types.RoleAssistant,
						Content: []types.ContentPart{
							types.ToolCallContent{
								ToolCallID: "call_a",
								ToolName:   "search",
								Arguments:  map[string]interface{}{"query": "first"},
								ProviderOptions: map[string]interface{}{
									"openai": map[string]interface{}{"itemId": "fc_a"},
								},
							},
							types.ToolCallContent{
								ToolCallID: "call_b",
								ToolName:   "search",
								Arguments:  map[string]interface{}{"query": "second"},
								ProviderOptions: map[string]interface{}{
									"openai": map[string]interface{}{"itemId": "fc_b"},
								},
							},
						},
					},
					{
						Role: types.RoleTool,
						Content: []types.ContentPart{
							types.ToolResultContent{
								ToolCallID: "call_a",
								ToolName:   "search",
								Output:     &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"results": []interface{}{}}},
							},
							types.ToolResultContent{
								ToolCallID: "call_b",
								ToolName:   "search",
								Output:     &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"results": []interface{}{"x"}}},
							},
						},
					},
				},
			}, "system", ConvertOptions{Store: store})
			if err != nil {
				t.Fatalf("conversion failed: %v", err)
			}
			if len(input) != 4 {
				t.Fatalf("input len = %d, want four function call/output items: %#v", len(input), input)
			}
			for i, wantCallID := range []string{"call_a", "call_b"} {
				call, ok := input[i].(FunctionCallItem)
				if !ok {
					t.Fatalf("input[%d] = %#v, want FunctionCallItem", i, input[i])
				}
				if call.ID != "" || call.CallID != wantCallID || call.Name != "search" {
					t.Fatalf("input[%d] = %#v, want client function_call without item id", i, input[i])
				}
			}
			for i, wantCallID := range []string{"call_a", "call_b"} {
				output, ok := input[i+2].(FunctionCallOutputItem)
				if !ok {
					t.Fatalf("input[%d] = %#v, want FunctionCallOutputItem", i+2, input[i+2])
				}
				if output.CallID != wantCallID {
					t.Fatalf("input[%d] = %#v, want output for %s", i+2, input[i+2], wantCallID)
				}
			}
		})
	}
}

func TestConvertPromptToInput_StoredOpenAISpecialAssistantToolCalls(t *testing.T) {
	t.Parallel()

	input, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				ToolCalls: []types.ToolCall{
					{
						ID:       "shell-call",
						ToolName: "openai.shell",
						ProviderMetadata: map[string]interface{}{
							"openai": map[string]interface{}{"itemId": "shell-item"},
						},
					},
					{
						ID:       "patch-call",
						ToolName: "openai.apply_patch",
						ProviderMetadata: map[string]interface{}{
							"openai": map[string]interface{}{"itemId": "patch-item"},
						},
					},
					{
						ID:       "custom-call",
						ToolName: "grammar_tool",
						ProviderMetadata: map[string]interface{}{
							"openai": map[string]interface{}{"itemId": "custom-item"},
						},
					},
				},
			},
		},
	}, "system", ConvertOptions{
		Store:             true,
		HasShellTool:      true,
		HasApplyPatchTool: true,
		CustomToolNames:   map[string]bool{"grammar_tool": true},
	})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if len(input) != 3 {
		t.Fatalf("input len = %d, want three item references: %#v", len(input), input)
	}
	for i, wantID := range []string{"shell-item", "patch-item", "custom-item"} {
		ref, ok := input[i].(map[string]interface{})
		if !ok || ref["type"] != "item_reference" || ref["id"] != wantID {
			t.Fatalf("input[%d] = %#v, want item_reference %s", i, input[i], wantID)
		}
	}

	skipped, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				ToolCalls: []types.ToolCall{{
					ID:       "custom-call",
					ToolName: "grammar_tool",
					ProviderMetadata: map[string]interface{}{
						"openai": map[string]interface{}{"itemId": "custom-item"},
					},
				}},
			},
		},
	}, "system", ConvertOptions{
		Store:                 true,
		HasPreviousResponseID: true,
		CustomToolNames:       map[string]bool{"grammar_tool": true},
	})
	if err != nil {
		t.Fatalf("previous-response conversion failed: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("stored custom tool call with previous response id should be skipped, got %#v", skipped)
	}
}

func TestConvertPromptToInput_StoredAssistantToolCallContentParts(t *testing.T) {
	t.Parallel()

	input, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ToolCallContent{
						ToolCallID: "call-1",
						ToolName:   "local_shell",
						ProviderOptions: map[string]interface{}{
							"openai": map[string]interface{}{"itemId": "lsh_123"},
						},
					},
					types.ToolCallContent{
						ToolCallID: "call-2",
						ToolName:   "apply_patch",
						ProviderOptions: map[string]interface{}{
							"openai": map[string]interface{}{"itemId": "apc_123"},
						},
					},
					types.ToolCallContent{
						ToolCallID: "call-3",
						ToolName:   "grammar_tool",
						ProviderOptions: map[string]interface{}{
							"openai": map[string]interface{}{"itemId": "ct_123"},
						},
					},
				},
			},
		},
	}, "system", ConvertOptions{
		Store:             true,
		HasLocalShellTool: true,
		HasApplyPatchTool: true,
		CustomToolNames:   map[string]bool{"grammar_tool": true},
	})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if len(input) != 3 {
		t.Fatalf("input len = %d, want three item references: %#v", len(input), input)
	}
	for i, wantID := range []string{"lsh_123", "apc_123", "ct_123"} {
		ref, ok := input[i].(map[string]interface{})
		if !ok || ref["type"] != "item_reference" || ref["id"] != wantID {
			t.Fatalf("input[%d] = %#v, want item_reference %s", i, input[i], wantID)
		}
	}
}

func TestConvertPromptToInput_StoredAssistantToolResultReferencesItem(t *testing.T) {
	t.Parallel()

	input, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "call-1",
						ToolName:   "provider_tool",
						Output:     &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "done"},
						ProviderOptions: map[string]interface{}{
							"openai": map[string]interface{}{"itemId": "tool-result-item"},
						},
					},
				},
			},
		},
	}, "system", ConvertOptions{Store: true})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if len(input) != 1 {
		t.Fatalf("input len = %d, want 1 item_reference: %#v", len(input), input)
	}
	ref, ok := input[0].(map[string]interface{})
	if !ok || ref["type"] != "item_reference" || ref["id"] != "tool-result-item" {
		t.Fatalf("input[0] = %#v, want item_reference tool-result-item", input[0])
	}

	conversationInput, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "call-1",
						ProviderOptions: map[string]interface{}{
							"openai": map[string]interface{}{"itemId": "tool-result-item"},
						},
					},
				},
			},
		},
	}, "system", ConvertOptions{Store: true, HasConversation: true})
	if err != nil {
		t.Fatalf("conversion with conversation failed: %v", err)
	}
	if len(conversationInput) != 0 {
		t.Fatalf("conversation input len = %d, want stored tool result skipped: %#v", len(conversationInput), conversationInput)
	}
}

func TestConvertPromptToInput_OpenAICompactionCustomContent(t *testing.T) {
	t.Parallel()

	prompt := types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.CustomContent{
						Kind: "openai-compaction",
						ProviderOptions: map[string]interface{}{
							"openai": map[string]interface{}{
								"itemId":           "compaction-1",
								"encryptedContent": "encrypted-context",
							},
						},
					},
				},
			},
		},
	}

	input, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if len(input) != 1 {
		t.Fatalf("input len = %d, want compaction item: %#v", len(input), input)
	}
	compaction, ok := input[0].(map[string]interface{})
	if !ok || compaction["type"] != "compaction" || compaction["id"] != "compaction-1" || compaction["encrypted_content"] != "encrypted-context" {
		t.Fatalf("input[0] = %#v, want compaction item", input[0])
	}

	stored, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{Store: true})
	if err != nil {
		t.Fatalf("stored conversion failed: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored input len = %d, want item_reference: %#v", len(stored), stored)
	}
	ref, ok := stored[0].(map[string]interface{})
	if !ok || ref["type"] != "item_reference" || ref["id"] != "compaction-1" {
		t.Fatalf("stored[0] = %#v, want item_reference compaction-1", stored[0])
	}
}

func TestOpenAIResponsesImageDetailHelper(t *testing.T) {
	t.Parallel()

	if got := openAIResponsesImageDetail(nil); got != "" {
		t.Fatalf("nil provider options should return empty detail, got %q", got)
	}
	if got := openAIResponsesImageDetail(map[string]interface{}{"openai": map[string]interface{}{"imageDetail": "high"}}); got != "high" {
		t.Fatalf("imageDetail key parse failed: %q", got)
	}
	if got := openAIResponsesImageDetail(map[string]interface{}{"openai": map[string]interface{}{"detail": "low"}}); got != "low" {
		t.Fatalf("detail fallback parse failed: %q", got)
	}
	if got := openAIResponsesImageDetail(map[string]interface{}{"azure": map[string]interface{}{"imageDetail": "auto"}}, "azure"); got != "auto" {
		t.Fatalf("provider-specific imageDetail parse failed: %q", got)
	}
}

func TestConvertPromptToInput_ToolCallsWithoutItemIDDoNotSetID(t *testing.T) {
	t.Parallel()

	input, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				ToolCalls: []types.ToolCall{
					{
						ID:               "search-call",
						ToolName:         "tool_search",
						Arguments:        map[string]interface{}{"paths": []string{"weather"}},
						ProviderMetadata: map[string]interface{}{},
					},
					{
						ID:        "local-call",
						ToolName:  "openai.local_shell",
						Arguments: map[string]interface{}{"action": map[string]interface{}{"command": []interface{}{}}},
					},
					{
						ID:        "shell-call",
						ToolName:  "openai.shell",
						Arguments: map[string]interface{}{"action": map[string]interface{}{"commands": []interface{}{}}},
					},
					{
						ID:        "patch-call",
						ToolName:  "openai.apply_patch",
						Arguments: map[string]interface{}{"callId": "call-explicit", "operation": map[string]interface{}{}},
					},
					{
						ID:           "custom-call",
						ToolName:     "grammar_tool",
						Arguments:    map[string]interface{}{"input": "value"},
						RawArguments: "{\"input\":\"value\"}",
					},
					{
						ID:        "fn-call",
						ToolName:  "weather",
						Arguments: map[string]interface{}{"city": "SF"},
					},
				},
			},
		},
	}, "system", ConvertOptions{
		HasLocalShellTool: true,
		HasShellTool:      true,
		HasApplyPatchTool: true,
		CustomToolNames:   map[string]bool{"grammar_tool": true},
	})
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if len(input) != 6 {
		t.Fatalf("input len = %d, want six tool-call items", len(input))
	}

	for i, item := range input {
		data, err := json.Marshal(item)
		if err != nil {
			t.Fatalf("failed to marshal item[%d]: %v", i, err)
		}
		s := string(data)
		if strings.Contains(s, "\"id\":\"\"") {
			t.Fatalf("item[%d] includes empty id unexpectedly: %s", i, s)
		}
	}

	search, ok := input[0].(ToolSearchCallItem)
	if !ok || search.ID != "" || search.CallID != nil {
		t.Fatalf("input[0] = %#v, want tool_search_call without item id and nil call_id", input[0])
	}
	local, ok := input[1].(LocalShellCall)
	if !ok || local.ID != "" || local.CallID != "local-call" {
		t.Fatalf("input[1] = %#v, want local_shell_call without item id", input[1])
	}
	shell, ok := input[2].(ShellCall)
	if !ok || shell.ID != "" || shell.CallID != "shell-call" {
		t.Fatalf("input[2] = %#v, want shell_call without item id", input[2])
	}
	patch, ok := input[3].(ApplyPatchCall)
	if !ok || patch.ID != nil || patch.CallID != "call-explicit" {
		t.Fatalf("input[3] = %#v, want apply_patch_call with nil item id and callId from args", input[3])
	}
	custom, ok := input[4].(CustomToolCallItem)
	if !ok || custom.ID != "" || custom.CallID != "custom-call" {
		t.Fatalf("input[4] = %#v, want custom_tool_call without item id", input[4])
	}
	function, ok := input[5].(FunctionCallItem)
	if !ok || function.ID != "" || function.CallID != "fn-call" {
		t.Fatalf("input[5] = %#v, want function_call without item id", input[5])
	}
}
