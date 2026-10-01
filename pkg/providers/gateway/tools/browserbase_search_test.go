package tools

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestNewBrowserbaseSearchSchemaAndProviderExecutedStub ports TS
// "creates a provider-executed Browserbase Search tool" and "describes the
// Browserbase Search API input constraints" (browserbase-search.test.ts).
func TestNewBrowserbaseSearchSchemaAndProviderExecutedStub(t *testing.T) {
	tool := NewBrowserbaseSearch(BrowserbaseSearchConfig{}).ToTool()
	if tool.Name != "browserbase_search" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Title != "Browserbase Search" || !tool.ProviderExecuted {
		t.Fatalf("tool metadata = %#v", tool)
	}
	if tool.Type != types.ToolTypeProviderDefined || tool.ProviderID != "gateway.browserbase_search" {
		t.Fatalf("provider tool identity = type:%q id:%q", tool.Type, tool.ProviderID)
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
	for _, name := range []string{"query", "num_results"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("missing property %q in %#v", name, properties)
		}
	}

	output, ok := tool.OutputSchema.(map[string]interface{})
	if !ok {
		t.Fatalf("output schema type = %T", tool.OutputSchema)
	}
	variants, ok := output["oneOf"].([]interface{})
	if !ok || len(variants) != 2 {
		t.Fatalf("output schema should match TS success/error union: %#v", output)
	}
	success := variants[0].(map[string]interface{})
	successRequired := success["required"].([]string)
	if !containsString(successRequired, "requestId") {
		t.Fatalf("success schema should require requestId: %#v", success)
	}
	errorSchema := variants[1].(map[string]interface{})["properties"].(map[string]interface{})
	errorEnum := errorSchema["error"].(map[string]interface{})["enum"].([]string)
	if !containsString(errorEnum, "timeout") {
		t.Fatalf("output error enum missing TS timeout: %#v", errorEnum)
	}

	result, err := tool.Execute(nil, nil, types.ToolExecutionOptions{ToolCallID: "call_1"})
	if err == nil || result != nil {
		t.Fatalf("Execute result=%#v err=%v, want provider-executed error", result, err)
	}
	toolErr, ok := err.(*types.ToolExecutionError)
	if !ok || !toolErr.ProviderExecuted || toolErr.ToolName != "browserbase_search" || toolErr.ToolCallID != "call_1" {
		t.Fatalf("tool error = %#v", err)
	}
}

// TestNewBrowserbaseSearchProviderArgs ports TS
// "creates a provider-executed Browserbase Search tool" numResults args
// assertion.
func TestNewBrowserbaseSearchProviderArgs(t *testing.T) {
	tool := NewBrowserbaseSearch(BrowserbaseSearchConfig{NumResults: intPtr(5)}).ToTool()
	if got, ok := tool.ProviderArgs["numResults"]; !ok || got != 5 {
		t.Fatalf("provider args = %#v, want numResults:5", tool.ProviderArgs)
	}
}

func TestBrowserbaseSearchProviderArgsOmitWhenUnset(t *testing.T) {
	tool := NewBrowserbaseSearch(BrowserbaseSearchConfig{}).ToTool()
	if len(tool.ProviderArgs) != 0 {
		t.Fatalf("provider args = %#v, want empty", tool.ProviderArgs)
	}
}
