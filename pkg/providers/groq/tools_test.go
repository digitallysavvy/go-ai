package groq

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func lookupTool() types.Tool {
	return types.Tool{
		Name:       "lookup",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}
}

func TestPrepareToolsBrowserSearchSupported(t *testing.T) {
	for _, id := range []string{"openai/gpt-oss-20b", "openai/gpt-oss-120b"} {
		tools, choice, warnings := prepareTools(id, []types.Tool{BrowserSearchTool()}, types.ToolChoice{Type: types.ToolChoiceRequired})
		want := []map[string]interface{}{{"type": "browser_search"}}
		if !reflect.DeepEqual(tools, want) {
			t.Fatalf("%s tools = %#v", id, tools)
		}
		if choice != "required" || len(warnings) != 0 {
			t.Fatalf("%s choice=%v warnings=%v", id, choice, warnings)
		}
	}
}

func TestPrepareToolsBrowserSearchKnownUnsupported(t *testing.T) {
	tools, _, warnings := prepareTools("gemma2-9b-it", []types.Tool{BrowserSearchTool()}, types.ToolChoice{})
	if len(tools) != 0 {
		t.Fatalf("tools = %#v", tools)
	}
	if len(warnings) != 1 || warnings[0].Type != "unsupported" ||
		warnings[0].Feature != "provider-defined tool groq.browser_search" ||
		warnings[0].Details != "Browser search is not supported on model gemma2-9b-it." {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestPrepareToolsBrowserSearchUnknownModelKept(t *testing.T) {
	for _, id := range []string{"openai/gpt-oss-240b", "future/browser-model", "new-model"} {
		tools, choice, warnings := prepareTools(id,
			[]types.Tool{BrowserSearchTool(), lookupTool()}, types.ToolChoice{Type: types.ToolChoiceRequired})
		if len(tools) != 2 || tools[0]["type"] != "browser_search" || tools[1]["type"] != "function" {
			t.Fatalf("%s tools = %#v", id, tools)
		}
		if choice != "required" || len(warnings) != 0 {
			t.Fatalf("%s choice=%v warnings=%v", id, choice, warnings)
		}
	}
}

func TestPrepareToolsUnknownProviderToolWarns(t *testing.T) {
	tools, _, warnings := prepareTools("openai/gpt-oss-20b",
		[]types.Tool{{Type: types.ToolTypeProviderDefined, ProviderID: "other.tool"}}, types.ToolChoice{})
	if len(tools) != 0 || len(warnings) != 1 || warnings[0].Feature != "provider-defined tool other.tool" {
		t.Fatalf("tools=%v warnings=%v", tools, warnings)
	}
}

func TestPrepareToolsNamedToolChoice(t *testing.T) {
	_, choice, _ := prepareTools("openai/gpt-oss-20b", []types.Tool{lookupTool()}, types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "lookup"})
	want := map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "lookup"}}
	if !reflect.DeepEqual(choice, want) {
		t.Fatalf("choice = %#v", choice)
	}
}

func TestBuildRequestBodyBrowserSearchWarning(t *testing.T) {
	m := &LanguageModel{modelID: "llama-3.1-8b-instant"}
	body, warnings := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools:  []types.Tool{BrowserSearchTool()},
	}, false)
	if got := body["tools"].([]map[string]interface{}); len(got) != 0 {
		t.Fatalf("tools = %#v", got)
	}
	found := false
	for _, w := range warnings {
		if w.Feature == "provider-defined tool groq.browser_search" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %#v", warnings)
	}
}
