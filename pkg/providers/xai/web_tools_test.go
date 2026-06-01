package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestWebSearchToolShape(t *testing.T) {
	enableImage := true
	enableImageSearch := true
	config := WebSearchConfig{
		AllowedDomains:           []string{"example.com"},
		ExcludedDomains:          []string{"spam.com"},
		EnableImageSearch:        &enableImageSearch,
		EnableImageUnderstanding: &enableImage,
	}

	tool := WebSearch(config)
	if tool.Name != "xai.web_search" {
		t.Fatalf("tool name = %q", tool.Name)
	}
	if !tool.ProviderExecuted {
		t.Fatal("web search should be provider-executed")
	}

	options, ok := tool.ProviderOptions.(WebSearchConfig)
	if !ok {
		t.Fatalf("provider options type = %T", tool.ProviderOptions)
	}
	if len(options.AllowedDomains) != 1 || options.AllowedDomains[0] != "example.com" {
		t.Fatalf("allowed domains = %#v", options.AllowedDomains)
	}
	if options.EnableImageUnderstanding == nil || *options.EnableImageUnderstanding != true {
		t.Fatalf("image understanding option mismatch: %#v", options.EnableImageUnderstanding)
	}
	wire := convertXAIResponsesTool(tool).(map[string]interface{})
	if wire["enable_image_search"] != true {
		t.Fatalf("enable_image_search = %#v, want true", wire["enable_image_search"])
	}

	result, err := tool.Execute(nil, map[string]interface{}{"query": "x"}, types.ToolExecutionOptions{ToolCallID: "tc1"})
	if result != nil || err == nil {
		t.Fatalf("expected provider-executed noop behavior, got result=%v err=%v", result, err)
	}
}

func TestViewXVideoToolShape(t *testing.T) {
	tool := ViewXVideo()
	if tool.Name != "xai.view_x_video" {
		t.Fatalf("tool name = %q", tool.Name)
	}
	if !tool.ProviderExecuted {
		t.Fatal("view_x_video should be provider-executed")
	}
	if tool.Description == "" {
		t.Fatal("expected non-empty description")
	}
}
