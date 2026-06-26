package tools

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestNewExaSearchSchemaAndProviderExecutedStub(t *testing.T) {
	tool := NewExaSearch(ExaSearchConfig{}).ToTool()
	if tool.Name != "exa_search" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Title != "Exa Search" || !tool.ProviderExecuted {
		t.Fatalf("tool metadata = %#v", tool)
	}
	if tool.Type != types.ToolTypeProviderDefined || tool.ProviderID != "gateway.exa_search" {
		t.Fatalf("provider tool identity = type:%q id:%q", tool.Type, tool.ProviderID)
	}
	output, ok := tool.OutputSchema.(map[string]interface{})
	if !ok {
		t.Fatalf("output schema type = %T", tool.OutputSchema)
	}
	variantCount, ok := output["oneOf"].([]interface{})
	if !ok || len(variantCount) != 2 {
		t.Fatalf("output schema should match TS success/error union: %#v", output)
	}
	errorSchema := variantCount[1].(map[string]interface{})["properties"].(map[string]interface{})
	errorEnum := errorSchema["error"].(map[string]interface{})["enum"].([]string)
	if !containsString(errorEnum, "execution_error") {
		t.Fatalf("output error enum missing TS execution_error: %#v", errorEnum)
	}
	params, ok := tool.Parameters.(map[string]interface{})
	if !ok {
		t.Fatalf("parameters type = %T", tool.Parameters)
	}
	required := params["required"].([]string)
	if len(required) != 1 || required[0] != "query" {
		t.Fatalf("required = %#v", required)
	}
	properties := params["properties"].(map[string]interface{})
	for _, name := range []string{"query", "type", "num_results", "category", "user_location", "include_domains", "exclude_domains", "start_published_date", "end_published_date", "contents"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("missing property %q in %#v", name, properties)
		}
	}
	contents := properties["contents"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := contents["subpage_target"]; !ok {
		t.Fatalf("contents schema missing subpage_target: %#v", contents)
	}

	result, err := tool.Execute(nil, nil, types.ToolExecutionOptions{ToolCallID: "call_1"})
	if err == nil || result != nil {
		t.Fatalf("Execute result=%#v err=%v, want provider-executed error", result, err)
	}
	toolErr, ok := err.(*types.ToolExecutionError)
	if !ok || !toolErr.ProviderExecuted || toolErr.ToolName != "exa_search" || toolErr.ToolCallID != "call_1" {
		t.Fatalf("tool error = %#v", err)
	}
}

func TestNewExaSearchProviderArgs(t *testing.T) {
	tool := NewExaSearch(ExaSearchConfig{
		Type:               "fast",
		NumResults:         intPtr(7),
		Category:           "research paper",
		UserLocation:       "US",
		IncludeDomains:     []string{"nature.com"},
		ExcludeDomains:     []string{"example.com"},
		StartPublishedDate: "2026-01-01",
		EndPublishedDate:   "2026-06-01",
		Contents: &ExaSearchContentsConfig{
			Text: ExaSearchTextConfig{
				MaxCharacters:   intPtr(1200),
				IncludeHTMLTags: boolPtr(true),
				Verbosity:       "compact",
				IncludeSections: []string{"body"},
			},
			Highlights: map[string]interface{}{"query": "agent"},
			Subpages:   intPtr(2),
			Extras: &ExaSearchExtrasConfig{
				Links:      intPtr(3),
				ImageLinks: intPtr(1),
			},
		},
	}).ToTool()

	args := tool.ProviderArgs
	if args["type"] != "fast" || args["numResults"] != 7 || args["category"] != "research paper" || args["userLocation"] != "US" {
		t.Fatalf("top-level args = %#v", args)
	}
	if args["startPublishedDate"] != "2026-01-01" || args["endPublishedDate"] != "2026-06-01" {
		t.Fatalf("date args = %#v", args)
	}
	contents := args["contents"].(map[string]interface{})
	if contents["subpages"] != 2 {
		t.Fatalf("contents args = %#v", contents)
	}
	encoded, err := json.Marshal(contents["text"])
	if err != nil {
		t.Fatalf("marshal text config: %v", err)
	}
	var text map[string]interface{}
	if err := json.Unmarshal(encoded, &text); err != nil {
		t.Fatalf("unmarshal text config: %v", err)
	}
	if text["maxCharacters"] != float64(1200) || text["includeHtmlTags"] != true || text["verbosity"] != "compact" {
		t.Fatalf("text config json = %#v", text)
	}
	extras := contents["extras"].(map[string]interface{})
	if extras["links"] != 3 || extras["imageLinks"] != 1 {
		t.Fatalf("extras args = %#v", extras)
	}
}

func TestExaSearchProviderArgsPreserveExplicitZeroNumericConfig(t *testing.T) {
	tool := NewExaSearch(ExaSearchConfig{
		NumResults: intPtr(0),
		Contents: &ExaSearchContentsConfig{
			Text: ExaSearchTextConfig{
				MaxCharacters: intPtr(0),
			},
			Highlights: ExaSearchHighlightsConfig{
				MaxCharacters: intPtr(0),
			},
			MaxAgeHours:      intPtr(0),
			LivecrawlTimeout: intPtr(0),
			Subpages:         intPtr(0),
			Extras: &ExaSearchExtrasConfig{
				Links:      intPtr(0),
				ImageLinks: intPtr(0),
			},
		},
	}).ToTool()
	if got, ok := tool.ProviderArgs["numResults"]; !ok || got != 0 {
		t.Fatalf("provider args = %#v, want explicit numResults:0", tool.ProviderArgs)
	}
	contents := tool.ProviderArgs["contents"].(map[string]interface{})
	for _, name := range []string{"maxAgeHours", "livecrawlTimeout", "subpages"} {
		if got, ok := contents[name]; !ok || got != 0 {
			t.Fatalf("contents args = %#v, want explicit %s:0", contents, name)
		}
	}
	extras := contents["extras"].(map[string]interface{})
	for _, name := range []string{"links", "imageLinks"} {
		if got, ok := extras[name]; !ok || got != 0 {
			t.Fatalf("extras args = %#v, want explicit %s:0", extras, name)
		}
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
