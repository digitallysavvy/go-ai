package deepseek

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// PB-20 (provb2b-07): DeepSeek prompt checks are TS InvalidPromptError
// (convert-to-deepseek-chat-messages.ts:131, :300).
func TestR91provbDeepSeekInvalidPrompt(t *testing.T) {
	_, err := deepseekResolveUserImagePart(types.FileContent{MediaType: "image/png", URL: "https://example.com/" + strings.Repeat("a", 8200)}, "deepseek")
	var ip *providererrors.InvalidPromptError
	if !errors.As(err, &ip) || err.Error() != "Invalid prompt: DeepSeek image URLs must not exceed 8192 characters." {
		t.Fatalf("err = %v", err)
	}
}

// TS f9d43bdaa7: convert-to-deepseek-responses-input.test.ts (extracted at
// 2ebc78b654 with --expand-each --fn=convertToDeepSeekResponsesInput)
// replayed through convertToDeepSeekResponsesInput.
func TestR91provbTSCasesResponsesConvert(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/responses_convert.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		msgs, why := tsfixture.BuildPromptMessages(in["prompt"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		name, _ := in["providerOptionsName"].(string)
		input, warnings, err := convertToDeepSeekResponsesInput(msgs, name)
		if err != nil {
			return nil, nil, err
		}
		b, _ := json.Marshal(input)
		var out interface{}
		_ = json.Unmarshal(b, &out)
		return map[string]interface{}{"input": out, "inputWarnings": tsfixture.WarningsToTS(warnings)}, nil, nil
	}, nil)
}

func r91provbConvertJSON(t *testing.T, msgs []types.Message) (string, []types.Warning, error) {
	t.Helper()
	input, warnings, err := convertToDeepSeekResponsesInput(msgs, "deepseek")
	b, _ := json.Marshal(input)
	return string(b), warnings, err
}

// TS f9d43bdaa7 convert-to-deepseek-responses-input.test.ts cases the
// extractor cannot evaluate (it.each over helper values, error matchers),
// ported by hand.
func TestR91provbResponsesConvertHandPorted(t *testing.T) {
	user := func(parts ...types.ContentPart) []types.Message {
		return []types.Message{{Role: types.RoleUser, Content: parts}}
	}
	t.Run("unsupported file parts warn and are skipped", func(t *testing.T) {
		for _, f := range []types.FileContent{
			{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/document.pdf"}},
			{MediaType: "audio/wav", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}},
			{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeText, Text: "document"}},
			{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeText, Text: "not image bytes"}},
		} {
			got, warnings, err := r91provbConvertJSON(t, user(types.TextContent{Text: "Before"}, f, types.TextContent{Text: "After"}))
			if err != nil || got != `[{"content":[{"text":"Before","type":"input_text"},{"text":"After","type":"input_text"}],"role":"user"}]` ||
				len(warnings) != 1 || warnings[0].Feature != "user message part type: file" {
				t.Fatalf("%s: %s %v %v", f.MediaType, got, warnings, err)
			}
		}
	})
	t.Run("non-string reasoning item ids are omitted", func(t *testing.T) {
		for _, id := range []interface{}{nil, 123, false, []interface{}{}, map[string]interface{}{}} {
			got, _, err := r91provbConvertJSON(t, []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ReasoningContent{Text: "Let me think.", ProviderOptions: map[string]interface{}{"deepseek": map[string]interface{}{"itemId": id}}},
			}}})
			if err != nil || got != `[{"content":[{"text":"Let me think.","type":"reasoning_text"}],"type":"reasoning"}]` {
				t.Fatalf("%v: %s %v", id, got, err)
			}
		}
	})
	t.Run("invalid or missing web search metadata skips the call", func(t *testing.T) {
		for _, v := range []interface{}{nil, "invalid", []interface{}{}, map[string]interface{}{}, map[string]interface{}{"type": "function_call"}, "absent"} {
			po := map[string]interface{}{"deepseek": map[string]interface{}{"webSearchCall": v}}
			if v == "absent" {
				po = nil
			}
			got, warnings, err := r91provbConvertJSON(t, []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "ws_1", ToolName: "web_search", Input: "{}", ProviderExecuted: true, ProviderOptions: po},
			}}})
			if err != nil || got != `[]` || len(warnings) != 0 {
				t.Fatalf("%v: %s %v %v", v, got, warnings, err)
			}
		}
	})
	t.Run("client tool named web_search is a function call; string arguments kept", func(t *testing.T) {
		got, _, err := r91provbConvertJSON(t, []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "c1", ToolName: "web_search", Input: `{"query":"x"}`},
		}}})
		if err != nil || got != `[{"arguments":"{\"query\":\"x\"}","call_id":"c1","name":"web_search","type":"function_call"}]` {
			t.Fatalf("%s %v", got, err)
		}
	})
	t.Run("execution-denied reasons", func(t *testing.T) {
		for _, c := range []struct {
			out  types.ToolResultOutput
			want string
		}{
			{types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied}, "Tool call execution denied."},
			{types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: "User declined."}, "User declined."},
			{types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, ReasonSet: true}, ""},
		} {
			out := c.out
			got, _, err := r91provbConvertJSON(t, []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{
				types.ToolResultContent{ToolCallID: "call_1", ToolName: "weather", Output: &out},
			}}})
			want, _ := json.Marshal([]interface{}{map[string]interface{}{"type": "function_call_output", "call_id": "call_1", "output": c.want}})
			if err != nil || got != string(want) {
				t.Fatalf("%s %v", got, err)
			}
		}
	})
	t.Run("image errors", func(t *testing.T) {
		var uf *providererrors.UnsupportedFunctionalityError
		for _, mt := range []string{"image/svg+xml", "image/tiff"} {
			_, _, err := r91provbConvertJSON(t, user(types.FileContent{MediaType: mt, FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}}))
			if !errors.As(err, &uf) || uf.Message != "DeepSeek supports JPEG, PNG, GIF, and WebP image inputs." {
				t.Fatalf("%s: %v", mt, err)
			}
		}
		_, _, err := r91provbConvertJSON(t, user(types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"},
			ProviderOptions: map[string]interface{}{"deepseek": map[string]interface{}{"fileData": true}}}))
		if !errors.As(err, &uf) || uf.Functionality != "DeepSeek `fileData` image parts with the Responses API" {
			t.Fatalf("fileData: %v", err)
		}
		long := "https://example.com/" + strings.Repeat("a", 8192-len("https://example.com/"))
		if _, _, err := r91provbConvertJSON(t, user(types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: long}})); err != nil {
			t.Fatalf("8192: %v", err)
		}
		var ip *providererrors.InvalidPromptError
		if _, _, err := r91provbConvertJSON(t, user(types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: long + "a"}})); !errors.As(err, &ip) {
			t.Fatalf("8193: %v", err)
		}
		var ia *providererrors.InvalidArgumentError
		if _, _, err := r91provbConvertJSON(t, user(types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://e/a.png"},
			ProviderOptions: map[string]interface{}{"deepseek": map[string]interface{}{"imageDetail": "ultra"}}})); !errors.As(err, &ia) {
			t.Fatalf("invalid options: %v", err)
		}
		if _, _, err := r91provbConvertJSON(t, user(types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeReference, Reference: map[string]string{"openai": "file-1"}}})); err == nil {
			t.Fatal("reference without a deepseek id accepted")
		}
	})
}

func init() {
	tsfixture.RegisterPorted(map[string]string{
		tsfixture.PortedKey("should accept image URLs at the 8192 character limit", "url.toString()", "toHaveLength"): "TestR91provbResponsesConvertHandPorted",
	})
}
