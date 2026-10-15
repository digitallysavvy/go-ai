package anthropic

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// tsProviderTool maps a TS provider tool to the Go tool. Go identifies
// provider tools by their TS id in Name.
func tsProviderTool(id, name string, args map[string]interface{}) (types.Tool, bool) {
	raw, _ := json.Marshal(args)
	decode := func(v interface{}) bool { return len(args) == 0 || json.Unmarshal(raw, v) == nil }
	var t types.Tool
	switch id {
	case "anthropic.web_search_20250305":
		var c tools.WebSearch20260209Config
		if !decode(&c) {
			return t, false
		}
		t = tools.WebSearch20250305(c)
	case "anthropic.web_search_20260209":
		var c tools.WebSearch20260209Config
		if !decode(&c) {
			return t, false
		}
		t = tools.WebSearch20260209(c)
	case "anthropic.web_search_20260318":
		var c tools.WebSearch20260318Config
		if !decode(&c) {
			return t, false
		}
		t = tools.WebSearch20260318(c)
	case "anthropic.web_fetch_20250910":
		var c tools.WebFetch20260209Config
		if !decode(&c) {
			return t, false
		}
		t = tools.WebFetch20250910(c)
	case "anthropic.web_fetch_20260209":
		var c tools.WebFetch20260209Config
		if !decode(&c) {
			return t, false
		}
		t = tools.WebFetch20260209(c)
	case "anthropic.web_fetch_20260318":
		var c tools.WebFetch20260318Config
		if !decode(&c) {
			return t, false
		}
		t = tools.WebFetch20260318(c)
	case "anthropic.advisor_20260301":
		var c tools.Advisor20260301Args
		if !decode(&c) {
			return t, false
		}
		t = tools.Advisor20260301(c)
	case "anthropic.computer_20241022", "anthropic.computer_20250124":
		var c tools.Computer20241022Args
		if !decode(&c) {
			return t, false
		}
		if id == "anthropic.computer_20241022" {
			t = tools.Computer20241022(c)
		} else {
			t = tools.Computer20250124(c)
		}
	case "anthropic.computer_20251124":
		var c tools.Computer20251124Args
		if !decode(&c) {
			return t, false
		}
		t = tools.Computer20251124(c)
	case "anthropic.text_editor_20250728":
		var c tools.TextEditor20250728Args
		if !decode(&c) {
			return t, false
		}
		t = tools.TextEditor20250728(c)
	case "anthropic.computer_toolset_20260801":
		if len(args) != 0 {
			return t, false
		}
		t = tools.ComputerToolset20260801(tools.ComputerToolset20260801Config{})
	default:
		if _, ok := anthropicBuiltinToolTypes[id]; !ok || len(args) != 0 {
			return t, false
		}
		t = types.Tool{Name: id, ProviderID: id, Type: "provider"}
	}
	tsfixture.ToolNameAlias[id], tsfixture.PromptToolNameAlias[name] = name, id
	return t, true
}

func TestTSCasesLanguage(t *testing.T) {
	tsfixture.ProviderToolHook = tsProviderTool
	tsfixture.ToolNameAlias, tsfixture.PromptToolNameAlias = map[string]string{}, map[string]string{}
	tsfixture.DropResponseTimestamp = true
	defer func() {
		tsfixture.ToolNameAlias, tsfixture.PromptToolNameAlias, tsfixture.DropResponseTimestamp = nil, nil, false
	}()
	cases := tsfixture.LoadCases(t, "testdata/tscases/language.json.gz")
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "claude-3-haiku-20240307"
		}
		m, err := New(Config{APIKey: "test-api-key", BaseURL: base}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, map[string]string{
		"should send the model id and settings":                                "TS passes topK 0.1; Go TopK is an int",
		"should not strip temperature / topP / topK when thinking is disabled": "TS passes topK 0.1; Go TopK is an int",
		"should pass headers":                                                  "the TS test sets provider-level headers on createAnthropic; the harness builds a default provider",
		"should process PDF citation responses":                                "the TS test's 'base64PDFdata' is not valid base64; Go decodes file data",
		"should process PDF citation responses in streaming":                   "the TS test's 'base64PDFdata' is not valid base64; Go decodes file data",
	})
}

const invalidBase64 = "the TS test passes strings like 'base64PDFdata' that are not valid base64; Go decodes file data"

// TestTSCasesConvertPrompt replays convert-to-anthropic-prompt.test.ts.
func TestTSCasesConvertPrompt(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_prompt.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		msgs, why := tsfixture.BuildPromptMessages(in["prompt"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		opts := prompt.AnthropicPromptOptions{}
		if tn, ok := in["toolsetNames"].(map[string]interface{}); ok {
			opts.ToolsetNames = map[string]string{}
			for k, v := range tn {
				opts.ToolsetNames[k] = fmt.Sprint(v)
			}
		}
		if v, ok := in["sendReasoning"].(bool); ok {
			opts.SendReasoning = &v
		}
		// toolNameMapping: tool name -> provider tool name via the tool's id
		if tm, ok := in["toolNameMapping"].(map[string]interface{}); ok {
			if m, ok := tm["__toolNameMapping"].(map[string]interface{}); ok {
				ids, _ := m["providerToolNames"].(map[string]interface{})
				custom := map[string]string{}
				tools, _ := m["tools"].([]interface{})
				for _, tv := range tools {
					tool, _ := tv.(map[string]interface{})
					if tool["type"] != "provider" {
						continue
					}
					if pn, ok := ids[fmt.Sprint(tool["id"])].(string); ok {
						custom[fmt.Sprint(tool["name"])] = pn
					}
				}
				opts.ToProviderToolName = func(name string) string {
					if pn, ok := custom[name]; ok {
						return pn
					}
					return name
				}
			}
		}
		out, err := prompt.ConvertToAnthropicPrompt(msgs, opts)
		if err != nil {
			return nil, nil, err
		}
		system, messages := out.System, out.Messages
		if messages == nil {
			messages = []map[string]interface{}{}
		}
		warnings := tsfixture.WarningsToTS(out.Warnings)
		betas := out.Betas
		if betas == nil {
			betas = []string{}
		}
		return map[string]interface{}{
			"prompt": map[string]interface{}{"system": system, "messages": messages},
			"betas":  map[string]interface{}{"__set": betas},
		}, map[string]interface{}{"warnings": warnings}, nil
	}, map[string]string{
		"should add PDF file parts for base64 PDFs":                          invalidBase64,
		"should not include citations by default":                            invalidBase64,
		"should include citations when enabled on file part":                 invalidBase64,
		"should include custom title and context when provided":              invalidBase64,
		"should handle multiple documents with consistent citation settings": invalidBase64,
	})
}

// TestTSCasesPrepareTools replays anthropic-prepare-tools.test.ts.
func TestTSCasesPrepareTools(t *testing.T) {
	tsfixture.ProviderToolHook = tsProviderTool
	tsfixture.ToolNameAlias, tsfixture.PromptToolNameAlias = map[string]string{}, map[string]string{}
	defer func() {
		tsfixture.ProviderToolHook, tsfixture.ToolNameAlias, tsfixture.PromptToolNameAlias = nil, nil, nil
	}()
	cases := tsfixture.LoadCases(t, "testdata/tscases/prepare_tools.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		o := prepareToolsOptions{
			supportsStructuredOutput: in["supportsStructuredOutput"] == true,
			supportsStrictTools:      in["supportsStrictTools"] == true,
			rejectsForcedToolUse:     in["rejectsForcedToolUse"] == true,
		}
		if tools, ok := in["tools"]; ok {
			ts, why := tsfixture.BuildTools(tools)
			if why != "" {
				return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
			}
			o.tools = ts
		}
		if tc, ok := in["toolChoice"].(map[string]interface{}); ok {
			choice := tsfixture.BuildToolChoice(tc)
			o.toolChoice = &choice
		}
		if v, ok := in["disableParallelToolUse"].(bool); ok {
			o.disableParallelToolUse = &v
		}
		out := prepareTools(o)
		if out.err != nil {
			return nil, nil, out.err
		}
		warnings := tsfixture.WarningsToTS(out.warnings)
		betas := out.betas
		if betas == nil {
			betas = []string{}
		}
		return map[string]interface{}{
			"tools":        out.tools,
			"tool_choice":  out.toolChoice,
			"toolChoice":   out.toolChoice,
			"toolWarnings": warnings,
			"betas":        map[string]interface{}{"__set": betas},
		}, nil, nil
	}, map[string]string{
		"should correctly prepare computer_20251124 tool": "enableZoom is a Go bool: unset is sent as enable_zoom false (same API default)",
	})
}
