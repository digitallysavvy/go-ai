package prompt

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r84provaGoogleOpts(kv map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"google": kv}
}

// TestR84provaGoogleEmptyThoughtSignatureForwarded: TS reads
// `providerOpts?.thoughtSignature != null ? String(...)`, so an empty string
// is sent as is, and on a Gemini 3 function call it is a signature: no
// sentinel, no warning. An empty serverToolCallId is still a server tool call.
func TestR84provaGoogleEmptyThoughtSignatureForwarded(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.TextContent{Text: "a", ProviderOptions: r84provaGoogleOpts(map[string]interface{}{"thoughtSignature": ""})},
			types.ReasoningContent{Text: "r", ProviderOptions: r84provaGoogleOpts(map[string]interface{}{"thoughtSignature": ""})},
			types.ToolCallContent{ToolCallID: "c1", ToolName: "f", Input: `{}`,
				ProviderOptions: r84provaGoogleOpts(map[string]interface{}{"thoughtSignature": ""})},
			types.ToolCallContent{ToolCallID: "c2", ToolName: "g", Input: `{}`,
				ProviderOptions: r84provaGoogleOpts(map[string]interface{}{"serverToolCallId": "", "serverToolType": "GOOGLE_SEARCH_WEB", "thoughtSignature": "s"})},
		}},
	}
	out, err := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{IsGemini3Model: true, IncludeFunctionCallIDs: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", out.Warnings)
	}
	parts, _ := out.Contents[1]["parts"].([]map[string]interface{})
	want := []map[string]interface{}{
		{"text": "a", "thoughtSignature": ""},
		{"text": "r", "thought": true, "thoughtSignature": ""},
		{"functionCall": map[string]interface{}{"id": "c1", "name": "f", "args": parts[2]["functionCall"].(map[string]interface{})["args"]}, "thoughtSignature": ""},
		{"toolCall": map[string]interface{}{"toolType": "GOOGLE_SEARCH_WEB", "args": parts[3]["toolCall"].(map[string]interface{})["args"], "id": ""}, "thoughtSignature": "s"},
	}
	if !reflect.DeepEqual(parts, want) {
		t.Errorf("parts =\n%#v\nwant\n%#v", parts, want)
	}
}

// TestR84provaGoogleReferenceNeedsFullMediaType: TS user file parts with a
// provider reference take resolveFullMediaType, which throws for a
// top-level-only type (not inline bytes); assistant parts send the type as
// given.
func TestR84provaGoogleReferenceNeedsFullMediaType(t *testing.T) {
	ref := types.FileContent{MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"google": "files/abc"}}}
	_, err := ConvertToGoogleMessages([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{ref}}}, GoogleMessagesOptions{})
	const want = `'file of media type "image" must specify subtype since it is not passed as inline bytes' functionality not supported.`
	if err == nil || err.Error() != want {
		t.Errorf("user err = %v, want %q", err, want)
	}
	out, err := ConvertToGoogleMessages([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{ref}},
	}, GoogleMessagesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	parts, _ := out.Contents[1]["parts"].([]map[string]interface{})
	if got := parts[0]["fileData"]; !reflect.DeepEqual(got, map[string]interface{}{"mimeType": "image", "fileUri": "files/abc"}) {
		t.Errorf("assistant fileData = %#v", got)
	}
}
