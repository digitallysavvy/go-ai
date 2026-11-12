package deepseek

import (
	"encoding/json"
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS 2ebc78b654 convert-to-deepseek-responses-input.test.ts: every it.each
// row the case extractor could not expand (round 91 covered them only
// through grouped tests), one subtest per row, named by the TS title with
// the row's value. Expected values are the TS inline snapshots.

func r92provbConvert(t *testing.T, msgs []types.Message, name string) (string, []types.Warning, error) {
	t.Helper()
	input, warnings, err := convertToDeepSeekResponsesInput(msgs, name)
	b, _ := json.Marshal(input)
	return string(b), warnings, err
}

func r92provbUser(parts ...types.ContentPart) []types.Message {
	return []types.Message{{Role: types.RoleUser, Content: parts}}
}

func r92provbWant(t *testing.T, got string, warnings []types.Warning, err error, want string, wantWarnings ...string) {
	t.Helper()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var g, w interface{}
	_ = json.Unmarshal([]byte(got), &g)
	if json.Unmarshal([]byte(want), &w) != nil {
		t.Fatalf("bad want %s", want)
	}
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if string(gb) != string(wb) {
		t.Fatalf("input\n got %s\nwant %s", gb, wb)
	}
	if len(warnings) != len(wantWarnings) {
		t.Fatalf("warnings = %+v, want %v", warnings, wantWarnings)
	}
	for i, ww := range wantWarnings {
		if warnings[i].Type != "unsupported" || warnings[i].Feature != ww {
			t.Fatalf("warning %d = %+v, want %q", i, warnings[i], ww)
		}
	}
}

func TestR92provbResponsesConvertEachRows(t *testing.T) {
	pngURL := func(mediaType string) types.FileContent {
		return types.FileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/image.png"}}
	}
	inline := func(mediaType string, data interface{}) types.FileContent {
		fd := types.FileData{Type: types.FileDataTypeData}
		switch d := data.(type) {
		case string:
			fd.DataString = d
		case []byte:
			fd.Data = d
		}
		return types.FileContent{MediaType: mediaType, FileData: fd}
	}

	// TS 121: should preserve text and image URL order for %s.
	for _, mt := range []string{"image/png", "image", "image/*"} {
		t.Run("should preserve text and image URL order for "+mt, func(t *testing.T) {
			got, w, err := r92provbConvert(t, r92provbUser(types.TextContent{Text: "Describe this image."}, pngURL(mt), types.TextContent{Text: "Be brief."}), "deepseek")
			r92provbWant(t, got, w, err, `[{"role":"user","content":[{"type":"input_text","text":"Describe this image."},{"type":"input_image","image_url":"https://example.com/image.png"},{"type":"input_text","text":"Be brief."}]}]`)
		})
	}

	// TS 172: should convert an inline image from $name.
	for _, c := range []struct {
		name string
		data interface{}
	}{{"base64", "AQID"}, {"Uint8Array", []byte{1, 2, 3}}} {
		t.Run("should convert an inline image from "+c.name, func(t *testing.T) {
			got, w, err := r92provbConvert(t, r92provbUser(inline("image/png", c.data)), "deepseek")
			r92provbWant(t, got, w, err, `[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,AQID"}]}]`)
		})
	}

	// TS 240: should detect the image subtype for %s inline data.
	for _, mt := range []string{"image", "image/*"} {
		t.Run("should detect the image subtype for "+mt+" inline data", func(t *testing.T) {
			got, w, err := r92provbConvert(t, r92provbUser(inline(mt, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})), "deepseek")
			r92provbWant(t, got, w, err, `[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,iVBORw0KGgo="}]}]`)
		})
	}

	// TS 283: should reject %s inline data whose subtype cannot be detected
	// (UnsupportedFunctionalityError).
	for _, mt := range []string{"image", "image/*"} {
		t.Run("should reject "+mt+" inline data whose subtype cannot be detected", func(t *testing.T) {
			_, _, err := r92provbConvert(t, r92provbUser(inline(mt, "AQID")), "deepseek")
			var uf *providererrors.UnsupportedFunctionalityError
			if !errors.As(err, &uf) {
				t.Fatalf("err = %T %v", err, err)
			}
		})
	}

	// TS 306: should reject unsupported inline image format %s.
	for _, mt := range []string{"image/svg+xml", "image/tiff"} {
		t.Run("should reject unsupported inline image format "+mt, func(t *testing.T) {
			_, _, err := r92provbConvert(t, r92provbUser(inline(mt, "AQID")), "deepseek")
			var uf *providererrors.UnsupportedFunctionalityError
			if !errors.As(err, &uf) || uf.Message != "DeepSeek supports JPEG, PNG, GIF, and WebP image inputs." {
				t.Fatalf("err = %v", err)
			}
		})
	}

	// TS 331: should resolve DeepSeek image references with providerOptionsName %s.
	for _, name := range []string{"deepseek", "custom"} {
		t.Run("should resolve DeepSeek image references with providerOptionsName "+name, func(t *testing.T) {
			ref := types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeReference, Reference: map[string]string{"deepseek": "file-image", "openai": "file-other"}}}
			got, w, err := r92provbConvert(t, r92provbUser(ref), name)
			r92provbWant(t, got, w, err, `[{"role":"user","content":[{"type":"input_image","file_id":"file-image"}]}]`)
		})
	}

	// TS 395: should forward imageDetail %s for URL and inline images.
	for _, detail := range []string{"low", "high", "original", "auto"} {
		t.Run("should forward imageDetail "+detail+" for URL and inline images", func(t *testing.T) {
			po := map[string]interface{}{"deepseek": map[string]interface{}{"imageDetail": detail}}
			u := pngURL("image/png")
			u.ProviderOptions = po
			d := inline("image/png", "AQID")
			d.ProviderOptions = po
			got, w, err := r92provbConvert(t, r92provbUser(u, d), "deepseek")
			r92provbWant(t, got, w, err, `[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/image.png","detail":"`+detail+`"},{"type":"input_image","image_url":"data:image/png;base64,AQID","detail":"`+detail+`"}]}]`)
		})
	}

	// TS 591: should warn and skip unsupported $mediaType $data.type file parts.
	for _, c := range []struct {
		title string
		part  types.FileContent
	}{
		{"'application/pdf' 'url'", types.FileContent{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/document.pdf"}}},
		{"'audio/wav' 'data'", types.FileContent{MediaType: "audio/wav", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}}},
		{"'text/plain' 'text'", types.FileContent{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeText, Text: "document"}}},
		{"'image/png' 'text'", types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeText, Text: "not image bytes"}}},
	} {
		t.Run("should warn and skip unsupported "+c.title+" file parts", func(t *testing.T) {
			got, w, err := r92provbConvert(t, r92provbUser(types.TextContent{Text: "Before"}, c.part, types.TextContent{Text: "After"}), "deepseek")
			r92provbWant(t, got, w, err, `[{"role":"user","content":[{"type":"input_text","text":"Before"},{"type":"input_text","text":"After"}]}]`, "user message part type: file")
		})
	}

	// TS 710: should preserve reasoning item IDs from providerOptionsName %s.
	for _, name := range []string{"deepseek", "custom"} {
		t.Run("should preserve reasoning item IDs from providerOptionsName "+name, func(t *testing.T) {
			msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{types.ReasoningContent{Text: "Let me think.", ProviderOptions: map[string]interface{}{
				name: map[string]interface{}{"itemId": "rs_1"}, "other": map[string]interface{}{"itemId": "wrong-id"},
			}}}}}
			got, w, err := r92provbConvert(t, msgs, name)
			r92provbWant(t, got, w, err, `[{"type":"reasoning","id":"rs_1","content":[{"type":"reasoning_text","text":"Let me think."}]}]`)
		})
	}

	// TS 752: should omit a non-string reasoning item ID: $itemId.
	for _, c := range []struct {
		title string
		id    interface{}
	}{{"null", nil}, {"123", 123}, {"false", false}, {"[]", []interface{}{}}, {"{}", map[string]interface{}{}}} {
		t.Run("should omit a non-string reasoning item ID: "+c.title, func(t *testing.T) {
			msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{types.ReasoningContent{Text: "Let me think.", ProviderOptions: map[string]interface{}{
				"deepseek": map[string]interface{}{"itemId": c.id},
			}}}}}
			got, w, err := r92provbConvert(t, msgs, "deepseek")
			r92provbWant(t, got, w, err, `[{"type":"reasoning","content":[{"type":"reasoning_text","text":"Let me think."}]}]`)
		})
	}

	// TS 922: should replay a web search call from providerOptionsName %s and
	// skip its result.
	for _, name := range []string{"deepseek", "custom"} {
		t.Run("should replay a web search call from providerOptionsName "+name+" and skip its result", func(t *testing.T) {
			call := map[string]interface{}{"type": "web_search_call", "id": "ws_1", "status": "completed", "action": map[string]interface{}{"type": "search", "queries": []interface{}{"Paris weather"}}}
			msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "ws_1", ToolName: "search", Input: "{}", ProviderExecuted: true, ProviderOptions: map[string]interface{}{name: map[string]interface{}{"webSearchCall": call}}},
				types.ToolResultContent{ToolCallID: "ws_1", ToolName: "search", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"status": "completed"}}},
				types.TextContent{Text: "It is sunny."},
			}}}
			got, w, err := r92provbConvert(t, msgs, name)
			r92provbWant(t, got, w, err, `[{"action":{"queries":["Paris weather"],"type":"search"},"id":"ws_1","status":"completed","type":"web_search_call"},{"role":"assistant","content":"It is sunny."}]`)
		})
	}

	// TS 985: should skip provider-executed calls with invalid web search
	// metadata: $webSearchCall.
	for _, c := range []struct {
		title string
		v     interface{}
	}{{"null", nil}, {"'invalid'", "invalid"}, {"[]", []interface{}{}}, {"{}", map[string]interface{}{}}, {"{ type: 'function_call' }", map[string]interface{}{"type": "function_call"}}} {
		t.Run("should skip provider-executed calls with invalid web search metadata: "+c.title, func(t *testing.T) {
			msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "ws_1", ToolName: "web_search", Input: "{}", ProviderExecuted: true, ProviderOptions: map[string]interface{}{"deepseek": map[string]interface{}{"webSearchCall": c.v}}},
			}}}
			got, w, err := r92provbConvert(t, msgs, "deepseek")
			r92provbWant(t, got, w, err, `[]`)
		})
	}

	toolMsg := func(out types.ToolResultOutput) []types.Message {
		return []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "call_1", ToolName: "weather", Output: &out}}}}
	}

	// TS 1101: should convert %s output (text, error-text).
	for _, typ := range []types.ToolResultOutputType{types.ToolResultOutputText, types.ToolResultOutputErrorText} {
		t.Run("should convert "+string(typ)+" output", func(t *testing.T) {
			got, w, err := r92provbConvert(t, toolMsg(types.ToolResultOutput{Type: typ, Value: "Weather report"}), "deepseek")
			r92provbWant(t, got, w, err, `[{"call_id":"call_1","output":"Weather report","type":"function_call_output"}]`)
		})
	}

	// TS 1136: should convert %s output (json, error-json).
	for _, typ := range []types.ToolResultOutputType{types.ToolResultOutputJSON, types.ToolResultOutputErrorJSON} {
		t.Run("should convert "+string(typ)+" output", func(t *testing.T) {
			got, w, err := r92provbConvert(t, toolMsg(types.ToolResultOutput{Type: typ, Value: map[string]interface{}{"temperature": 20}}), "deepseek")
			r92provbWant(t, got, w, err, `[{"call_id":"call_1","output":"{\"temperature\":20}","type":"function_call_output"}]`)
		})
	}

	// TS 1171: should convert execution-denied output with reason $reason.
	for _, c := range []struct {
		title string
		out   types.ToolResultOutput
		want  string
	}{
		{"undefined", types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied}, "Tool call execution denied."},
		{"'User declined.'", types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: "User declined."}, "User declined."},
		{"''", types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, ReasonSet: true}, ""},
	} {
		t.Run("should convert execution-denied output with reason "+c.title, func(t *testing.T) {
			got, w, err := r92provbConvert(t, toolMsg(c.out), "deepseek")
			want, _ := json.Marshal([]interface{}{map[string]interface{}{"type": "function_call_output", "call_id": "call_1", "output": c.want}})
			r92provbWant(t, got, w, err, string(want))
		})
	}

	// TS 1208: should preserve text, inline images, URLs, and references
	// with providerOptionsName %s.
	for _, name := range []string{"deepseek", "custom"} {
		t.Run("should preserve text, inline images, URLs, and references with providerOptionsName "+name, func(t *testing.T) {
			out := types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
				types.TextContentBlock{Text: "Images:"},
				types.FileContentBlock{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}, ProviderOptions: map[string]interface{}{name: map[string]interface{}{"imageDetail": "low"}}},
				types.FileContentBlock{MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/image.png"}, ProviderOptions: map[string]interface{}{name: map[string]interface{}{"imageDetail": "high"}}},
				types.FileContentBlock{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeReference, Reference: map[string]string{"deepseek": "file-image"}}},
				types.TextContentBlock{Text: "Done."},
			}}
			msgs := []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "call_1", ToolName: "inspect", Output: &out}}}}
			got, w, err := r92provbConvert(t, msgs, name)
			r92provbWant(t, got, w, err, `[{"call_id":"call_1","type":"function_call_output","output":[{"text":"Images:","type":"input_text"},{"detail":"low","image_url":"data:image/png;base64,AQID","type":"input_image"},{"detail":"high","image_url":"https://example.com/image.png","type":"input_image"},{"file_id":"file-image","type":"input_image"},{"text":"Done.","type":"input_text"}]}]`)
		})
	}
}
