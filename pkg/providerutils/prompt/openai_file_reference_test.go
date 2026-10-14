package prompt

import (
	"errors"
	"reflect"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from TS #22466 (convert-to-openai-compatible-chat-messages.test.ts):
// provider-reference file parts become file_id parts.

func referenceFile(mediaType string, ref types.ProviderReference) types.FileContent {
	return types.FileContent{
		FileData:        types.FileData{Type: types.FileDataTypeReference, Reference: ref},
		MediaType:       mediaType,
		Filename:        "document.pdf",
		ProviderOptions: map[string]interface{}{"openaiCompatible": map[string]interface{}{"customOption": "value"}},
	}
}

func TestToOpenAIMessagesWithError_ResolvesProviderReferences(t *testing.T) {
	for _, mediaType := range []string{"application/pdf", "image/png"} {
		t.Run(mediaType, func(t *testing.T) {
			got, err := ToOpenAIMessagesWithError([]types.Message{{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					referenceFile(mediaType, types.ProviderReference{"custom-provider": "file-123", "openai": "file-other"}),
				},
			}}, ToOpenAIMessagesOptions{Provider: "custom-provider"})
			if err != nil {
				t.Fatal(err)
			}
			want := []map[string]interface{}{{
				"role": "user",
				"content": []map[string]interface{}{{
					"type":         "file",
					"file":         map[string]interface{}{"file_id": "file-123"},
					"customOption": "value",
				}},
			}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %#v\nwant %#v", got, want)
			}
		})
	}
}

func TestToOpenAIMessagesWithError_RejectsReferenceWithoutProviderID(t *testing.T) {
	ref := types.ProviderReference{"openai": "file-other"}
	_, err := ToOpenAIMessagesWithError([]types.Message{{
		Role:    types.RoleUser,
		Content: []types.ContentPart{referenceFile("application/pdf", ref)},
	}}, ToOpenAIMessagesOptions{Provider: "custom-provider"})
	var noSuch *providererrors.NoSuchProviderReferenceError
	if !errors.As(err, &noSuch) || noSuch.Provider != "custom-provider" || !reflect.DeepEqual(noSuch.Reference, ref) {
		t.Fatalf("err = %#v", err)
	}
	if err := ValidateProviderFileReferences([]types.Message{{
		Role:    types.RoleUser,
		Content: []types.ContentPart{referenceFile("application/pdf", ref)},
	}}, "custom-provider"); err == nil {
		t.Error("ValidateProviderFileReferences: want error")
	}
}

func TestToOpenAIMessagesWithError_ToolContentReference(t *testing.T) {
	got, err := ToOpenAIMessagesWithError([]types.Message{{
		Role: types.RoleTool,
		Content: []types.ContentPart{types.ToolResultContent{
			ToolCallID: "call-1",
			ToolName:   "t",
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
				types.FileContentBlock{
					FileData:  types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"custom-provider": "file-123"}},
					MediaType: "application/pdf",
				},
			}},
		}},
	}}, ToOpenAIMessagesOptions{Provider: "custom-provider", SupportsMultiPartToolContent: true})
	if err != nil {
		t.Fatal(err)
	}
	parts, _ := got[0]["content"].([]map[string]interface{})
	want := []map[string]interface{}{{"type": "file", "file": map[string]interface{}{"file_id": "file-123"}}}
	if !reflect.DeepEqual(parts, want) {
		t.Errorf("content = %#v, want %#v", got[0]["content"], want)
	}
}

// Without Provider the converter keeps its legacy behavior and never errors.
func TestToOpenAIMessages_ReferenceWithoutProviderKeepsLegacyShape(t *testing.T) {
	msgs := []types.Message{{
		Role: types.RoleUser,
		Content: []types.ContentPart{types.FileContent{
			Reference: "file-legacy", MediaType: "application/pdf",
		}},
	}}
	got, err := ToOpenAIMessagesWithError(msgs)
	if err != nil {
		t.Fatal(err)
	}
	part := got[0]["content"].([]map[string]interface{})[0]
	if part["file"].(map[string]interface{})["file_id"] != "file-legacy" {
		t.Errorf("part = %#v", part)
	}
}
