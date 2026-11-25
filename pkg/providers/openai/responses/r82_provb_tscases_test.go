package responses

import (
	"encoding/json"
	"fmt"
	"regexp"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r82provbStringSet reads a TS Set<string> argument ({"__set": [...]}).
func r82provbStringSet(v interface{}) map[string]bool {
	m, _ := v.(map[string]interface{})
	list, _ := m["__set"].([]interface{})
	if list == nil {
		return nil
	}
	out := map[string]bool{}
	for _, x := range list {
		if s, ok := x.(string); ok {
			out[s] = true
		}
	}
	return out
}

// TestR82provbTSCasesConvertInput replays convert-to-openai-responses-input
// .test.ts (TS convertToOpenAIResponsesInput, extracted at e64ed82f27 with
// --expand-each --fn=convertToOpenAIResponsesInput) through
// ConvertPromptToInputWithOptions. The TS test wrapper defaults
// toolSearchToolName to "tool_search"; its testToolNameMapping is the
// identity.
func TestR82provbTSCasesConvertInput(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_input.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		msgs, why := tsfixture.BuildPromptMessages(in["prompt"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		// toolNameMapping: the identity, or `name === 'a' ? 'b' : name`
		// (custom -> provider name), recorded as the function's source.
		var providerToolNames map[string]string
		if m, ok := in["toolNameMapping"].(map[string]interface{}); ok {
			fn, _ := m["toProviderToolName"].(map[string]interface{})
			src, _ := fn["src"].(string)
			for _, sm := range r82provbTernaryMapping.FindAllStringSubmatch(src, -1) {
				if providerToolNames == nil {
					providerToolNames = map[string]string{}
				}
				providerToolNames[sm[1]] = sm[2]
			}
		}
		b := func(k string) bool { v, _ := in[k].(bool); return v }
		opts := ConvertOptions{
			PassThroughUnsupportedFiles:  b("passThroughUnsupportedFiles"),
			HasPreviousResponseID:        b("hasPreviousResponseId"),
			HasConversation:              b("hasConversation"),
			Store:                        b("store"),
			CustomToolNames:              r82provbStringSet(in["customProviderToolNames"]),
			HasLocalShellTool:            b("hasLocalShellTool"),
			HasShellTool:                 b("hasShellTool"),
			HasApplyPatchTool:            b("hasApplyPatchTool"),
			HasComputerTool:              b("hasComputerTool"),
			IsReasoningModel:             b("isReasoningModel"),
			ExplicitMessageItemType:      b("explicitMessageItemType"),
			OutputSchemaToolNames:        r82provbStringSet(in["outputSchemaToolNames"]),
			ToolSearchToolName:           "tool_search",
			ProviderToolNames:            providerToolNames,
			IncludePromptCacheBreakpoint: true,
		}
		opts.ProviderOptionsName, _ = in["providerOptionsName"].(string)
		opts.ConfigurationUpdateUnsupportedReason, _ = in["configurationUpdateUnsupportedReason"].(string)
		if s, ok := in["toolSearchToolName"].(string); ok {
			opts.ToolSearchToolName = s
		}
		if list, ok := in["fileIdPrefixes"].([]interface{}); ok {
			for _, x := range list {
				if s, ok := x.(string); ok {
					opts.FileIDPrefixes = append(opts.FileIDPrefixes, s)
				}
			}
		}
		mode, _ := in["systemMessageMode"].(string)
		input, warnings, err := ConvertPromptToInputWithOptions(types.Prompt{Messages: msgs}, mode, opts)
		if err != nil {
			return nil, nil, err
		}
		raw, _ := json.Marshal(input)
		var out interface{}
		_ = json.Unmarshal(raw, &out)
		if out == nil {
			out = []interface{}{}
		}
		return map[string]interface{}{"input": out, "warnings": tsfixture.WarningsToTS(warnings)}, nil, nil
	}, map[string]string{
		"should convert single tool result part with json value":                  "tool output JSON text keeps TS object key order, which a Go map cannot",
		"should convert multiple tool result parts in a single message":           "tool output JSON text keeps TS object key order, which a Go map cannot",
		"should replay program items and preserve nested function caller linkage": "tool output JSON text keeps TS object key order, which a Go map cannot",
	})
}

var r82provbTernaryMapping = regexp.MustCompile(`===\s*'([^']+)'\s*\?\s*'([^']+)'`)

// TestR82provbTSCasesXAIInput replays convert-to-xai-responses-input.test.ts
// (TS convertToXaiResponsesInput, extracted at e64ed82f27 with --expand-each
// --fn=convertToXaiResponsesInput) through ConvertPromptToXAIInput.
func TestR82provbTSCasesXAIInput(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/xai_input.json.gz")
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
		raw, _ := json.Marshal(input)
		var out interface{}
		_ = json.Unmarshal(raw, &out)
		if out == nil {
			out = []interface{}{}
		}
		return map[string]interface{}{"input": out, "inputWarnings": tsfixture.WarningsToTS(warnings)}, nil, nil
	}, map[string]string{})
}
