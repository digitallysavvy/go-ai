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
	if len(args) == 0 {
		if _, ok := anthropicBuiltinToolTypes[id]; ok {
			tsfixture.ToolNameAlias[id], tsfixture.PromptToolNameAlias[name] = name, id
			return types.Tool{Name: id, ProviderID: id, Type: "provider"}, true
		}
		var t types.Tool
		switch id {
		case "anthropic.web_fetch_20260209":
			t = tools.WebFetch20260209(tools.WebFetch20260209Config{})
		case "anthropic.computer_toolset_20260801":
			t = tools.ComputerToolset20260801(tools.ComputerToolset20260801Config{})
		default:
			return t, false
		}
		tsfixture.ToolNameAlias[id], tsfixture.PromptToolNameAlias[name] = name, id
		return t, true
	}
	raw, _ := json.Marshal(args)
	var t types.Tool
	switch id {
	case "anthropic.web_search_20250305":
		var c tools.WebSearch20260209Config
		if json.Unmarshal(raw, &c) != nil {
			return t, false
		}
		t = tools.WebSearch20250305(c)
	case "anthropic.web_fetch_20250910":
		var c tools.WebFetch20260209Config
		if json.Unmarshal(raw, &c) != nil {
			return t, false
		}
		t = tools.WebFetch20250910(c)
	case "anthropic.advisor_20260301":
		var c tools.Advisor20260301Args
		if json.Unmarshal(raw, &c) != nil {
			return t, false
		}
		t = tools.Advisor20260301(c)
	default:
		return t, false
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
		warnings := make([]interface{}, 0, len(out.Warnings))
		for _, w := range out.Warnings {
			warnings = append(warnings, w)
		}
		betas := out.Betas
		if betas == nil {
			betas = []string{}
		}
		return map[string]interface{}{
			"prompt": map[string]interface{}{"system": system, "messages": messages},
			"betas":  map[string]interface{}{"__set": betas},
		}, map[string]interface{}{"warnings": warnings}, nil
	}, map[string]string{
		"should add PDF file parts for base64 PDFs": "the TS test passes 'base64PDFdata', which is not valid base64; Go decodes file data",
	})
}
