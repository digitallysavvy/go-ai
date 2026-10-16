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

func compatUserFile(f types.FileContent) []types.Message {
	return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{f}}}
}

func TestToOpenAIMessagesWithError_CompatFileValidation(t *testing.T) {
	opt := ToOpenAIMessagesOptions{CompatFileValidation: true, SpreadCompatMetadata: true}
	cases := []struct {
		name string
		file types.FileContent
		want string
	}{
		{"audio media type", types.FileContent{MediaType: "audio/ogg", Data: []byte("x")}, "audio media type audio/ogg"},
		{"audio url", types.FileContent{MediaType: "audio/wav", URL: "https://x/a.wav"}, "audio file parts with URLs"},
		{"pdf url", types.FileContent{MediaType: "application/pdf", URL: "https://x/a.pdf"}, "PDF file parts with URLs"},
		{"application type", types.FileContent{MediaType: "application/zip", Data: []byte("x")}, "file part media type application/zip"},
		{"text file part", types.FileContent{FileData: types.FileData{Type: types.FileDataTypeText, Text: "hi"}, MediaType: "text/plain"}, "text file parts"},
		{"other top level", types.FileContent{MediaType: "model/gltf", Data: []byte("x")}, "file part media type model/gltf"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ToOpenAIMessagesWithError(compatUserFile(tc.file), opt)
			var ufe *providererrors.UnsupportedFunctionalityError
			if !errors.As(err, &ufe) || ufe.Functionality != tc.want {
				t.Fatalf("err = %v, want unsupported %q", err, tc.want)
			}
			// the exported lenient form still returns messages
			if got := ToOpenAIMessages(compatUserFile(tc.file), opt); len(got) != 1 {
				t.Fatalf("lenient result = %v", got)
			}
		})
	}
	// Without the option nothing is rejected.
	if _, err := ToOpenAIMessagesWithError(compatUserFile(cases[0].file)); err != nil {
		t.Fatal(err)
	}
}

func TestToOpenAIMessagesWithError_CompatTextFile(t *testing.T) {
	opt := ToOpenAIMessagesOptions{CompatFileValidation: true, SpreadCompatMetadata: true}
	got, err := ToOpenAIMessagesWithError(compatUserFile(types.FileContent{MediaType: "text/plain", Data: []byte("hello"),
		ProviderOptions: map[string]interface{}{"openaiCompatible": map[string]interface{}{"k": "v"}}}), opt)
	if err != nil {
		t.Fatal(err)
	}
	parts := got[0]["content"].([]map[string]interface{})
	want := map[string]interface{}{"type": "text", "text": "hello", "k": "v"}
	if !reflect.DeepEqual(parts[0], want) {
		t.Fatalf("got %v want %v", parts[0], want)
	}
	got, _ = ToOpenAIMessagesWithError(compatUserFile(types.FileContent{MediaType: "text/csv", URL: "https://x/a.csv"}), opt)
	if p := got[0]["content"].([]map[string]interface{})[0]; p["type"] != "text" || p["text"] != "https://x/a.csv" {
		t.Fatalf("url text part = %v", p)
	}
	// valid audio/pdf still convert
	if _, err := ToOpenAIMessagesWithError(compatUserFile(types.FileContent{MediaType: "audio/mpeg", Data: []byte("x")}), opt); err != nil {
		t.Fatal(err)
	}
	if _, err := ToOpenAIMessagesWithError(compatUserFile(types.FileContent{MediaType: "application/pdf", Data: []byte("x")}), opt); err != nil {
		t.Fatal(err)
	}
}

func TestToOpenAIMessagesWithError_CompatSniffsBareMediaType(t *testing.T) {
	opt := ToOpenAIMessagesOptions{CompatFileValidation: true}
	pdf := []byte("%PDF-1.4 hello")
	got, err := ToOpenAIMessagesWithError(compatUserFile(types.FileContent{MediaType: "application", Data: pdf}), opt)
	if err != nil {
		t.Fatal(err)
	}
	f := got[0]["content"].([]map[string]interface{})[0]["file"].(map[string]interface{})
	if f["file_data"] == nil {
		t.Fatalf("file = %v", f)
	}
	png := []byte("\x89PNG\r\n\x1a\n0000")
	got, err = ToOpenAIMessagesWithError(compatUserFile(types.FileContent{MediaType: "image", Data: png}), opt)
	if err != nil {
		t.Fatal(err)
	}
	url := got[0]["content"].([]map[string]interface{})[0]["image_url"].(map[string]interface{})["url"].(string)
	if len(url) < 22 || url[:22] != "data:image/png;base64," {
		t.Fatalf("url = %s", url)
	}
	_, err = ToOpenAIMessagesWithError(compatUserFile(types.FileContent{MediaType: "audio", Data: []byte("zzzz")}), opt)
	var ufe *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &ufe) {
		t.Fatalf("err = %v", err)
	}
}
