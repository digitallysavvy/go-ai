package tools

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestNewBrowserbaseFetchSchemaAndProviderExecutedStub ports TS
// "creates a provider-executed Browserbase Fetch tool" and "describes all
// Browserbase Fetch API inputs" (browserbase-fetch.test.ts).
func TestNewBrowserbaseFetchSchemaAndProviderExecutedStub(t *testing.T) {
	tool := NewBrowserbaseFetch(BrowserbaseFetchConfig{}).ToTool()
	if tool.Name != "browserbase_fetch" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Title != "Browserbase Fetch" || !tool.ProviderExecuted {
		t.Fatalf("tool metadata = %#v", tool)
	}
	if tool.Type != types.ToolTypeProviderDefined || tool.ProviderID != "gateway.browserbase_fetch" {
		t.Fatalf("provider tool identity = type:%q id:%q", tool.Type, tool.ProviderID)
	}
	params, ok := tool.Parameters.(map[string]interface{})
	if !ok {
		t.Fatalf("parameters type = %T", tool.Parameters)
	}
	required := params["required"].([]string)
	if len(required) != 1 || required[0] != "url" {
		t.Fatalf("required = %#v", required)
	}
	properties := params["properties"].(map[string]interface{})
	for _, name := range []string{"url", "allow_redirects", "allow_insecure_ssl", "proxies", "format", "schema"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("missing property %q in %#v", name, properties)
		}
	}
	formatProp := properties["format"].(map[string]interface{})
	formatEnum := formatProp["enum"].([]string)
	for _, want := range []string{"raw", "json", "markdown"} {
		if !containsString(formatEnum, want) {
			t.Fatalf("format enum missing %q: %#v", want, formatEnum)
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
	for _, want := range []string{"id", "content", "contentType", "encoding", "headers", "statusCode"} {
		if !containsString(successRequired, want) {
			t.Fatalf("success schema should require %q: %#v", want, success)
		}
	}
	errorSchema := variants[1].(map[string]interface{})["properties"].(map[string]interface{})
	errorEnum := errorSchema["error"].(map[string]interface{})["enum"].([]string)
	if !containsString(errorEnum, "rate_limit") {
		t.Fatalf("output error enum missing TS rate_limit: %#v", errorEnum)
	}

	result, err := tool.Execute(nil, nil, types.ToolExecutionOptions{ToolCallID: "call_1"})
	if err == nil || result != nil {
		t.Fatalf("Execute result=%#v err=%v, want provider-executed error", result, err)
	}
	toolErr, ok := err.(*types.ToolExecutionError)
	if !ok || !toolErr.ProviderExecuted || toolErr.ToolName != "browserbase_fetch" || toolErr.ToolCallID != "call_1" {
		t.Fatalf("tool error = %#v", err)
	}
}

// TestNewBrowserbaseFetchProviderArgs ports TS
// "creates a provider-executed Browserbase Fetch tool" args assertion.
func TestNewBrowserbaseFetchProviderArgs(t *testing.T) {
	tool := NewBrowserbaseFetch(BrowserbaseFetchConfig{
		AllowRedirects: boolPtr(true),
		Format:         "markdown",
		Proxies:        boolPtr(true),
	}).ToTool()

	args := tool.ProviderArgs
	if got, ok := args["allowRedirects"]; !ok || got != true {
		t.Fatalf("allowRedirects = %#v", args)
	}
	if got, ok := args["format"]; !ok || got != "markdown" {
		t.Fatalf("format = %#v", args)
	}
	if got, ok := args["proxies"]; !ok || got != true {
		t.Fatalf("proxies = %#v", args)
	}
	if _, ok := args["allowInsecureSsl"]; ok {
		t.Fatalf("allowInsecureSsl should be omitted when unset: %#v", args)
	}
}

func TestBrowserbaseFetchProviderArgsOmitWhenUnset(t *testing.T) {
	tool := NewBrowserbaseFetch(BrowserbaseFetchConfig{}).ToTool()
	if len(tool.ProviderArgs) != 0 {
		t.Fatalf("provider args = %#v, want empty", tool.ProviderArgs)
	}
}
