package prompt

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from packages/google/src/google-file-inspection.test.ts (TS
// afb4e69924), the GenerateContent half.

const portprovgFileURI = "https://generativelanguage.googleapis.com/v1beta/files/inspection"

type portprovgInspectionCase struct {
	name      string
	mediaType string
	metadata  map[string]interface{}
}

func portprovgInspectionCases() []portprovgInspectionCase {
	return []portprovgInspectionCase{
		{"PDF pages", "application/pdf", map[string]interface{}{
			"mediaResolution":  map[string]interface{}{"level": "MEDIA_RESOLUTION_HIGH"},
			"documentMetadata": map[string]interface{}{"startPage": 3, "endPage": 4},
		}},
		{"Word text layers", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", map[string]interface{}{
			"documentMetadata": map[string]interface{}{"startPage": 2, "endPage": 3, "contentLayers": []interface{}{"PAGE_TEXT"}},
		}},
		{"PowerPoint visual layers", "application/vnd.openxmlformats-officedocument.presentationml.presentation", map[string]interface{}{
			"documentMetadata": map[string]interface{}{"startPage": 4, "endPage": 5, "contentLayers": []interface{}{"PAGE_IMAGE"}},
		}},
		{"spreadsheet cells and formulas", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", map[string]interface{}{
			"spreadsheetMetadata": map[string]interface{}{"sheetIndex": 3, "visibleCells": "A1:C5", "contentLayers": []interface{}{"DATA", "FORMULAS"}},
		}},
		{"OpenDocument sheet rendering", "application/vnd.oasis.opendocument.spreadsheet", map[string]interface{}{
			"spreadsheetMetadata": map[string]interface{}{"sheetIndex": 1, "visibleCells": "B5", "contentLayers": []interface{}{"VISUAL"}},
		}},
		{"video frames", "video/mp4", map[string]interface{}{
			"videoMetadata": map[string]interface{}{"startOffset": "10s", "endOffset": "20s", "fps": 2, "contentLayers": []interface{}{"FRAMES"}},
		}},
		{"audio clip starting at zero", "audio/mpeg", map[string]interface{}{
			"audioMetadata": map[string]interface{}{"startOffset": "0s", "endOffset": "30.5s"},
		}},
	}
}

func portprovgJSON(t *testing.T, v interface{}) interface{} {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPortprovgGoogleFileInspectionSerializes(t *testing.T) {
	type dataCase struct {
		name   string
		data   types.FileData
		inline bool
	}
	dataCases := []dataCase{
		{"bytes", types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}}, true},
		{"base64", types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}, true},
		{"uploaded URL", types.FileData{Type: types.FileDataTypeURL, URL: portprovgFileURI}, false},
		{"provider reference", types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"google": portprovgFileURI}}, false},
	}
	for _, toolResult := range []bool{false, true} {
		for _, dc := range dataCases {
			for _, ic := range portprovgInspectionCases() {
				name := ic.name + "/" + dc.name
				if toolResult {
					name = "tool result/" + name
				}
				t.Run(name, func(t *testing.T) {
					var messages []types.Message
					if toolResult {
						messages = []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
							ToolCallID: "call-1", ToolName: "view_file",
							Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
								types.FileContentBlock{FileData: dc.data, MediaType: ic.mediaType, ProviderOptions: map[string]interface{}{"google": ic.metadata}},
							}},
						}}}}
					} else {
						messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
							types.FileContent{FileData: dc.data, MediaType: ic.mediaType, ProviderOptions: map[string]interface{}{"google": ic.metadata}},
						}}}
					}
					out, err := ConvertToGoogleMessages(messages, GoogleMessagesOptions{SupportsFunctionResponseParts: true, IncludeFunctionCallIDs: true})
					if err != nil {
						t.Fatal(err)
					}
					expected := map[string]interface{}{}
					if dc.inline {
						expected["inlineData"] = map[string]interface{}{"mimeType": ic.mediaType, "data": "AQID"}
					} else {
						expected["fileData"] = map[string]interface{}{"mimeType": ic.mediaType, "fileUri": portprovgFileURI}
					}
					for k, v := range ic.metadata {
						expected[k] = v
					}
					want := interface{}(expected)
					if toolResult {
						want = map[string]interface{}{"functionResponse": map[string]interface{}{
							"id": "call-1", "name": "view_file",
							"response": map[string]interface{}{"name": "view_file", "content": "Tool executed successfully."},
							"parts":    []interface{}{expected},
						}}
					}
					got := portprovgJSON(t, out.Contents[0]["parts"].([]map[string]interface{})[0])
					if !reflect.DeepEqual(got, portprovgJSON(t, want)) {
						t.Fatalf("part = %v\nwant   %v", got, portprovgJSON(t, want))
					}
				})
			}
		}
	}
}

// Vertex does not take a provider reference as function response file data
// (it stays text), and a Gemini Files API URL is only forwarded on the Gemini
// API (TS convert-to-google-messages.ts appendToolResultParts).
func TestPortprovgGoogleToolResultReferencesAndFilesURLs(t *testing.T) {
	convert := func(names []string, data types.FileData) string {
		out, err := ConvertToGoogleMessages([]types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
			ToolCallID: "c", ToolName: "t",
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
				types.FileContentBlock{FileData: data, MediaType: "application/pdf"},
			}},
		}}}}, GoogleMessagesOptions{SupportsFunctionResponseParts: true, ProviderOptionsNames: names})
		if err != nil {
			t.Fatal(err)
		}
		return googleJSON(t, out.Contents[0]["parts"])
	}
	ref := types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"google": portprovgFileURI}}
	url := types.FileData{Type: types.FileDataTypeURL, URL: portprovgFileURI}
	if got := convert(nil, ref); !strings.Contains(got, `"fileData":{"fileUri":"`+portprovgFileURI+`","mimeType":"application/pdf"}`) {
		t.Errorf("google reference = %s", got)
	}
	if got := convert([]string{"googleVertex", "vertex"}, ref); strings.Contains(got, "fileData") || !strings.Contains(got, "reference") {
		t.Errorf("vertex reference = %s", got)
	}
	if got := convert(nil, url); !strings.Contains(got, `"fileData"`) {
		t.Errorf("google Files API URL = %s", got)
	}
	if got := convert([]string{"googleVertex", "vertex"}, url); strings.Contains(got, `"fileData"`) {
		t.Errorf("vertex Files API URL = %s", got)
	}
}

func TestPortprovgGoogleFileInspectionRejectsMalformedOptions(t *testing.T) {
	for name, options := range map[string]map[string]interface{}{
		"unknown media resolution level": {"mediaResolution": map[string]interface{}{"level": "ULTRA"}},
		"page below one":                 {"documentMetadata": map[string]interface{}{"startPage": 0}},
		"fractional page":                {"documentMetadata": map[string]interface{}{"endPage": 1.5}},
		"unknown spreadsheet layer":      {"spreadsheetMetadata": map[string]interface{}{"contentLayers": []interface{}{"IMAGES"}}},
		"audio offset not a string":      {"audioMetadata": map[string]interface{}{"startOffset": 3}},
		"video fps above 24":             {"videoMetadata": map[string]interface{}{"fps": 25}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ConvertToGoogleMessages([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
				types.FileContent{MediaType: "application/pdf", Data: []byte{1}, ProviderOptions: map[string]interface{}{"google": options}},
			}}}, GoogleMessagesOptions{})
			var validation *providererrors.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("err = %v (%T)", err, err)
			}
		})
	}
	// Null options are dropped, unknown keys stripped.
	out, err := ConvertToGoogleMessages([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{MediaType: "application/pdf", Data: []byte{1}, ProviderOptions: map[string]interface{}{"google": map[string]interface{}{
			"documentMetadata": nil, "thoughtSignature": "x", "audioMetadata": map[string]interface{}{"endOffset": "1s", "extra": true},
		}}},
	}}}, GoogleMessagesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := googleJSON(t, out.Contents[0]["parts"]); !strings.Contains(got, `"audioMetadata":{"endOffset":"1s"}`) || strings.Contains(got, "documentMetadata") || strings.Contains(got, "thoughtSignature") {
		t.Fatalf("parts = %s", got)
	}
}
