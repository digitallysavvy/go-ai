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
