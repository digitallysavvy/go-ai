package perplexity

import (
	"encoding/base64"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// This file ports packages/perplexity/src/convert-to-perplexity-input.test.ts.

// TS: "converts system and text messages to Agent API input items"
func TestConvertToPerplexityInput_SystemAndText(t *testing.T) {
	input, warnings, err := convertToPerplexityInput([]types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Be concise."}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello "}, types.TextContent{Text: "world"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "Hello!"}}},
	})
	if err != nil {
		t.Fatalf("convertToPerplexityInput() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %+v, want none", warnings)
	}
	if len(input) != 3 {
		t.Fatalf("input = %+v, want 3 items", input)
	}
	if input[0]["type"] != "message" || input[0]["role"] != "system" || input[0]["content"] != "Be concise." {
		t.Fatalf("input[0] = %+v", input[0])
	}
	if input[1]["content"] != "Hello world" {
		t.Fatalf("input[1] content = %v, want concatenated text", input[1]["content"])
	}
	if input[2]["role"] != "assistant" || input[2]["content"] != "Hello!" {
		t.Fatalf("input[2] = %+v", input[2])
	}
}

// TS: "converts image URLs and inline image data"
func TestConvertToPerplexityInput_Images(t *testing.T) {
	input, _, err := convertToPerplexityInput([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "Describe these images"},
			types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/image.png", MediaType: "image/png"}},
			types.FileContent{MediaType: "image/png", Data: []byte{0, 1, 2, 3}},
		}},
	})
	if err != nil {
		t.Fatalf("convertToPerplexityInput() error = %v", err)
	}
	content, ok := input[0]["content"].([]map[string]interface{})
	if !ok || len(content) != 3 {
		t.Fatalf("content = %#v, want 3-part multipart array", input[0]["content"])
	}
	if content[0]["type"] != "input_text" || content[0]["text"] != "Describe these images" {
		t.Fatalf("content[0] = %v", content[0])
	}
	if content[1]["type"] != "input_image" || content[1]["image_url"] != "https://example.com/image.png" {
		t.Fatalf("content[1] = %v", content[1])
	}
	wantData := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{0, 1, 2, 3})
	if content[2]["type"] != "input_image" || content[2]["image_url"] != wantData {
		t.Fatalf("content[2] = %v, want %q", content[2], wantData)
	}
}

// TS: "converts function calls and tool results for multi-turn input"
func TestConvertToPerplexityInput_FunctionCallsAndResults(t *testing.T) {
	input, _, err := convertToPerplexityInput([]types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{
				ToolCallID:      "call-1",
				ToolName:        "weather",
				Arguments:       map[string]interface{}{"city": "San Francisco"},
				ProviderOptions: map[string]interface{}{"perplexity": map[string]interface{}{"thoughtSignature": "signature-1"}},
			},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{
				ToolCallID: "call-1",
				ToolName:   "weather",
				Output:     &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"temperature": float64(18)}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("convertToPerplexityInput() error = %v", err)
	}
	if len(input) != 2 {
		t.Fatalf("input = %+v, want 2 items", input)
	}
	if input[0]["type"] != "function_call" || input[0]["call_id"] != "call-1" || input[0]["arguments"] != `{"city":"San Francisco"}` || input[0]["thought_signature"] != "signature-1" {
		t.Fatalf("input[0] = %+v", input[0])
	}
	if input[1]["type"] != "function_call_output" || input[1]["call_id"] != "call-1" || input[1]["output"] != `{"temperature":18}` {
		t.Fatalf("input[1] = %+v", input[1])
	}
}

// TS: "omits null thought signatures from multi-turn input"
func TestConvertToPerplexityInput_OmitsNullThoughtSignature(t *testing.T) {
	input, _, err := convertToPerplexityInput([]types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{"city": "San Francisco"}},
		}},
	})
	if err != nil {
		t.Fatalf("convertToPerplexityInput() error = %v", err)
	}
	if _, ok := input[0]["thought_signature"]; ok {
		t.Fatalf("thought_signature should be omitted, got %v", input[0]["thought_signature"])
	}
}

// TS: "warns when reasoning prompt parts cannot be replayed"
func TestConvertToPerplexityInput_ReasoningWarning(t *testing.T) {
	_, warnings, err := convertToPerplexityInput([]types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ReasoningContent{Text: "private reasoning"}}},
	})
	if err != nil {
		t.Fatalf("convertToPerplexityInput() error = %v", err)
	}
	if len(warnings) != 1 || warnings[0].Type != "unsupported" || warnings[0].Feature != "reasoning content in prompt" {
		t.Fatalf("warnings = %+v", warnings)
	}
}

// TS: "rejects PDFs because the Agent API only supports image input"
func TestConvertToPerplexityInput_RejectsPDF(t *testing.T) {
	_, _, err := convertToPerplexityInput([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{MediaType: "application/pdf", Data: []byte("%PDF-1.4")},
		}},
	})
	if err == nil {
		t.Fatal("expected UnsupportedFunctionalityError for PDF input")
	}
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("error = %v (%T), want UnsupportedFunctionalityError", err, err)
	}
}
