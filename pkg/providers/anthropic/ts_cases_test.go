package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// tsProviderTool maps a TS provider tool to the Go tool. Go identifies
// provider tools by their TS id in Name.
func tsProviderTool(id, name string, args map[string]interface{}) (types.Tool, bool) {
	if len(args) == 0 {
		if _, ok := anthropicBuiltinToolTypes[id]; ok {
			tsfixture.ToolNameAlias[id] = name
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
		tsfixture.ToolNameAlias[id] = name
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
	tsfixture.ToolNameAlias[id] = name
	return t, true
}

func TestTSCasesLanguage(t *testing.T) {
	tsfixture.ProviderToolHook = tsProviderTool
	tsfixture.ToolNameAlias = map[string]string{}
	tsfixture.DropResponseTimestamp = true
	defer func() { tsfixture.ToolNameAlias, tsfixture.DropResponseTimestamp = nil, false }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/language.json.gz")
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "claude-3-haiku-20240307"
		}
		m, err := New(Config{APIKey: "k", BaseURL: base}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, map[string]string{
		"should send the model id and settings":              "TS passes topK 0.1; Go TopK is an int",
		"should process PDF citation responses":              "the TS test's 'base64PDFdata' is not valid base64; Go decodes file data",
		"should process PDF citation responses in streaming": "the TS test's 'base64PDFdata' is not valid base64; Go decodes file data",
	})
}
