package responses

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r77provbNameMapping matches a toProviderToolName arrow function's
// `name === 'sdk' ? 'provider'` branch in the TS source of the arguments.
var r77provbNameMapping = regexp.MustCompile(`toProviderToolName:[^=]*=>[^']*'([^']+)'\s*\?\s*'([^']+)'`)

// r77provbStringSet reads a TS Set<string> argument ({__set: [...]}) or a
// string array.
func r77provbStringSet(v interface{}) map[string]bool {
	if m, ok := v.(map[string]interface{}); ok {
		v = m["__set"]
	}
	list, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := map[string]bool{}
	for _, e := range list {
		if s, ok := e.(string); ok {
			out[s] = true
		}
	}
	return out
}

// TestR77ProvbTSCasesConvertInput replays convert-to-openai-responses-input.test.ts
// (TS convertToOpenAIResponsesInput, extracted with --fn) through
// ConvertPromptToInputWithOptions: each case's prompt and options, compared
// on the returned input items and warnings. The TS test wraps the function
// with toolSearchToolName 'tool_search' and an identity toolNameMapping.
func TestR77ProvbTSCasesConvertInput(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_input.json.gz")
	srcByTest := map[string][]string{}
	for _, c := range cases {
		srcByTest[fmt.Sprint(c.FnArgs)] = c.FnArgSrc
	}
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		src := srcByTest[fmt.Sprint(args)]
		msgs, why := tsfixture.BuildPromptMessages(in["prompt"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		opts := ConvertOptions{ToolSearchToolName: "tool_search", IncludePromptCacheBreakpoint: true}
		b := func(k string) bool { v, _ := in[k].(bool); return v }
		opts.Store = b("store")
		opts.HasConversation = b("hasConversation")
		opts.HasPreviousResponseID = b("hasPreviousResponseId")
		opts.IsReasoningModel = b("isReasoningModel")
		opts.ExplicitMessageItemType = b("explicitMessageItemType")
		opts.HasLocalShellTool = b("hasLocalShellTool")
		opts.HasShellTool = b("hasShellTool")
		opts.HasApplyPatchTool = b("hasApplyPatchTool")
		opts.PassThroughUnsupportedFiles = b("passThroughUnsupportedFiles")
		opts.ProviderOptionsName, _ = in["providerOptionsName"].(string)
		opts.ConfigurationUpdateUnsupportedReason, _ = in["configurationUpdateUnsupportedReason"].(string)
		opts.CustomToolNames = r77provbStringSet(in["customProviderToolNames"])
		opts.OutputSchemaToolNames = r77provbStringSet(in["outputSchemaToolNames"])
		if v, ok := in["toolSearchToolName"].(string); ok {
			opts.ToolSearchToolName = v
		}
		if ps, ok := in["fileIdPrefixes"].([]interface{}); ok {
			for _, p := range ps {
				opts.FileIDPrefixes = append(opts.FileIDPrefixes, fmt.Sprint(p))
			}
		}
		// a toolNameMapping written as code (`toProviderToolName: name =>
		// name === 'program' ? 'programmatic_tool_calling' : name`)
		if len(src) > 0 {
			for _, m := range r77provbNameMapping.FindAllStringSubmatch(src[0], -1) {
				if opts.ProviderToolNames == nil {
					opts.ProviderToolNames = map[string]string{}
				}
				opts.ProviderToolNames[m[1]] = m[2]
			}
		}
		mode, _ := in["systemMessageMode"].(string)
		input, warnings, err := ConvertPromptToInputWithOptions(types.Prompt{Messages: msgs}, mode, opts)
		if err != nil {
			return nil, nil, err
		}
		if input == nil {
			input = []interface{}{}
		}
		return map[string]interface{}{"input": input, "warnings": tsfixture.WarningsToTS(warnings)}, nil, nil
	}, map[string]string{
		"should convert single tool result part with json value":                  "tool result JSON arrives as a Go map, which has no key order",
		"should convert multiple tool result parts in a single message":           "tool result JSON arrives as a Go map, which has no key order",
		"should replay program items and preserve nested function caller linkage": "tool result JSON arrives as a Go map, which has no key order",
	})
}

// TestR77ProvbReasoningEncryptedContentFromProviderOptions: TS reads a
// replayed reasoning part's encrypted content from
// providerOptions[provider].reasoningEncryptedContent, which is where response
// messages (and UI messages) carry the providerMetadata a Responses result
// reported. Go read only the Go-only EncryptedContent field, so with
// store: false a multi-step StreamText/GenerateText dropped the reasoning
// ("Reasoning parts without encrypted content are not supported ...").
func TestR77ProvbReasoningEncryptedContentFromProviderOptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		part types.ReasoningContent
		want interface{}
	}{
		{"providerOptions with itemId", types.ReasoningContent{Text: "r", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"itemId": "rs_1", "reasoningEncryptedContent": "enc_1"}}}, "enc_1"},
		{"providerOptions without itemId", types.ReasoningContent{Text: "r", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEncryptedContent": "enc_2"}}}, "enc_2"},
		{"providerMetadata", types.ReasoningContent{Text: "r", ProviderMetadata: []byte(`{"openai":{"itemId":"rs_3","reasoningEncryptedContent":"enc_3"}}`)}, "enc_3"},
		{"Go field", types.ReasoningContent{Text: "r", EncryptedContent: "enc_4", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"itemId": "rs_4", "reasoningEncryptedContent": nil}}}, "enc_4"},
		{"null is absent", types.ReasoningContent{Text: "r", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"itemId": "rs_5", "reasoningEncryptedContent": nil}}}, nil},
	} {
		input, _, err := ConvertPromptToInputWithOptions(types.Prompt{Messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{tc.part}}}}, "system", ConvertOptions{Store: false})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var got interface{}
		if len(input) == 1 {
			got = input[0].(map[string]interface{})["encrypted_content"]
		}
		if got != tc.want {
			t.Errorf("%s: encrypted_content = %v, want %v (input %v)", tc.name, got, tc.want, input)
		}
	}
}

// TestR77ProvbTSCasesXAIConvertInput replays xai
// convert-to-xai-responses-input.test.ts (TS convertToXaiResponsesInput)
// through ConvertPromptToXAIInput, compared on input and inputWarnings.
func TestR77ProvbTSCasesXAIConvertInput(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/xai_convert_input.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		msgs, why := tsfixture.BuildPromptMessages(in["prompt"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		input, warnings, err := ConvertPromptToXAIInput(types.Prompt{Messages: msgs})
		if err != nil {
			return nil, nil, err
		}
		if input == nil {
			input = []interface{}{}
		}
		return map[string]interface{}{"input": input, "inputWarnings": tsfixture.WarningsToTS(warnings)}, nil, nil
	}, nil)
}
