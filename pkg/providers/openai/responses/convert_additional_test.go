package responses

import (
	"encoding/base64"
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
			},
		},
	})
	parts, ok := out.([]CustomToolCallOutputPart)
	if !ok || len(parts) != 3 {
		t.Fatalf("expected 3 structured parts, got %#v", out)
	}
	if parts[1].Detail != "low" || parts[2].Filename != "x.json" {
		t.Fatalf("structured part fields mismatch: %#v", parts)
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
}
