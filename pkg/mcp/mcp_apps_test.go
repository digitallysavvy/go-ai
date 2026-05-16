package mcp

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMCPAppClientCapabilities(t *testing.T) {
	caps := MCPAppClientCapabilities()
	extension, ok := caps.Extensions[MCPAppExtensionName].(map[string]interface{})
	if !ok {
		t.Fatalf("missing MCP App extension: %#v", caps.Extensions)
	}
	mimeTypes, ok := extension["mimeTypes"].([]string)
	if !ok || len(mimeTypes) != 1 || mimeTypes[0] != MCPAppMimeType {
		t.Fatalf("mimeTypes = %#v, want [%s]", extension["mimeTypes"], MCPAppMimeType)
	}
}

func TestGetMCPAppToolMeta(t *testing.T) {
	tool := MCPTool{
		Name:        "showDashboard",
		InputSchema: map[string]interface{}{"type": "object"},
		Meta: map[string]interface{}{
			"ui": map[string]interface{}{
				"resourceUri": "ui://ai-sdk-e2e/dashboard",
				"visibility":  []interface{}{"model", "app", "invalid"},
			},
		},
	}

	meta, err := GetMCPAppToolMeta(tool)
	if err != nil {
		t.Fatalf("GetMCPAppToolMeta error: %v", err)
	}
	if meta.ResourceURI != "ui://ai-sdk-e2e/dashboard" {
		t.Fatalf("ResourceURI = %q", meta.ResourceURI)
	}
	if !reflect.DeepEqual(meta.Visibility, []string{"model", "app"}) {
		t.Fatalf("Visibility = %#v", meta.Visibility)
	}
	encoded, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("Marshal MCPAppToolMeta error: %v", err)
	}
	if string(encoded) != `{"resourceUri":"ui://ai-sdk-e2e/dashboard","visibility":["model","app"]}` {
		t.Fatalf("MCPAppToolMeta JSON = %s", encoded)
	}

	legacyURI, err := GetMCPAppResourceURI(MCPTool{
		Meta: map[string]interface{}{MCPAppLegacyResourceURIMetaKey: "ui://legacy/app"},
	})
	if err != nil || legacyURI != "ui://legacy/app" {
		t.Fatalf("legacy URI = %q, err=%v", legacyURI, err)
	}

	_, err = GetMCPAppResourceURI(MCPTool{
		Meta: map[string]interface{}{"ui": map[string]interface{}{"resourceUri": "https://example.com/app.html"}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid MCP App resource URI") {
		t.Fatalf("expected invalid URI error, got %v", err)
	}
}

func TestSplitAndDeduplicateMCPAppTools(t *testing.T) {
	plain := MCPTool{Name: "plainTool", InputSchema: map[string]interface{}{"type": "object"}}
	uiTool := MCPTool{
		Name:        "showDashboard",
		InputSchema: map[string]interface{}{"type": "object"},
		Meta: map[string]interface{}{"ui": map[string]interface{}{
			"resourceUri": "ui://ai-sdk-e2e/dashboard",
			"visibility":  []interface{}{"model", "app"},
		}},
	}
	appOnly := MCPTool{
		Name:        "refreshDashboardData",
		InputSchema: map[string]interface{}{"type": "object"},
		Meta: map[string]interface{}{"ui": map[string]interface{}{
			"resourceUri": "ui://ai-sdk-e2e/dashboard",
			"visibility":  []interface{}{"app"},
		}},
	}

	definitions := ListToolsResult{Tools: []MCPTool{plain, uiTool, appOnly}, NextCursor: "next"}
	modelVisible, appVisible, err := SplitMCPAppTools(definitions)
	if err != nil {
		t.Fatalf("SplitMCPAppTools error: %v", err)
	}
	if got := []string{modelVisible.Tools[0].Name, modelVisible.Tools[1].Name}; !reflect.DeepEqual(got, []string{"plainTool", "showDashboard"}) {
		t.Fatalf("model visible = %#v", got)
	}
	if got := []string{appVisible.Tools[0].Name, appVisible.Tools[1].Name}; !reflect.DeepEqual(got, []string{"showDashboard", "refreshDashboardData"}) {
		t.Fatalf("app visible = %#v", got)
	}
	if modelVisible.NextCursor != "next" {
		t.Fatalf("NextCursor = %q", modelVisible.NextCursor)
	}

	uris, err := GetMCPAppResourceURIs(definitions)
	if err != nil {
		t.Fatalf("GetMCPAppResourceURIs error: %v", err)
	}
	if !reflect.DeepEqual(uris, []string{"ui://ai-sdk-e2e/dashboard"}) {
		t.Fatalf("uris = %#v", uris)
	}
}

func TestGetMCPAppResourceFromReadResult(t *testing.T) {
	html := "<!doctype html><html>blob</html>"
	resource, err := GetMCPAppResourceFromReadResult("ui://ai-sdk-e2e/dashboard", ReadResourceResult{
		Contents: []ResourceContent{{
			URI:      "ui://ai-sdk-e2e/dashboard",
			MimeType: MCPAppMimeType,
			Blob:     base64.StdEncoding.EncodeToString([]byte(html)),
			Meta: map[string]interface{}{
				"ui": map[string]interface{}{"prefersBorder": true},
			},
		}},
	})
	if err != nil {
		t.Fatalf("GetMCPAppResourceFromReadResult error: %v", err)
	}
	if resource.HTML != html || resource.MimeType != MCPAppMimeType || resource.Meta["prefersBorder"] != true {
		t.Fatalf("resource = %#v", resource)
	}
}
