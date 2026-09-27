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

// TestGetMCPAppResourceFromReadResultDropsMalformedMetaFields mirrors TS
// mcp-apps.test.ts "drops malformed and non-string _meta.ui fields"
// (hash 48e7e78).
func TestGetMCPAppResourceFromReadResultDropsMalformedMetaFields(t *testing.T) {
	resource, err := GetMCPAppResourceFromReadResult("ui://ai-sdk-e2e/dashboard", ReadResourceResult{
		Contents: []ResourceContent{{
			URI:      "ui://ai-sdk-e2e/dashboard",
			MimeType: MCPAppMimeType,
			Text:     "<!doctype html>",
			Meta: map[string]interface{}{
				"ui": map[string]interface{}{
					"prefersBorder": "yes", // wrong type -> dropped
					"csp": map[string]interface{}{
						"connectDomains":  []interface{}{"https://ok.example", float64(42), nil}, // non-strings dropped
						"resourceDomains": "not-an-array",                                        // wrong type -> dropped
					},
					"permissions": "nope", // wrong type -> dropped
					"extra":       "kept", // unknown key passes through
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("GetMCPAppResourceFromReadResult error: %v", err)
	}
	if _, ok := resource.Meta["prefersBorder"]; ok {
		t.Fatalf("prefersBorder should be dropped: %#v", resource.Meta)
	}
	if _, ok := resource.Meta["permissions"]; ok {
		t.Fatalf("permissions should be dropped: %#v", resource.Meta)
	}
	if resource.Meta["extra"] != "kept" {
		t.Fatalf("unknown key should pass through: %#v", resource.Meta)
	}
	csp, ok := resource.Meta["csp"].(map[string]interface{})
	if !ok {
		t.Fatalf("csp should remain an object: %#v", resource.Meta)
	}
	connectDomains, ok := csp["connectDomains"].([]string)
	if !ok || len(connectDomains) != 1 || connectDomains[0] != "https://ok.example" {
		t.Fatalf("connectDomains = %#v, want [\"https://ok.example\"]", csp["connectDomains"])
	}
	if _, ok := csp["resourceDomains"]; ok {
		t.Fatalf("resourceDomains should be dropped: %#v", csp)
	}
}

// TestFingerprintMCPAppResource mirrors TS mcp-app-fingerprint.test.ts.
func TestFingerprintMCPAppResource(t *testing.T) {
	base := func(overrideMeta map[string]interface{}) MCPAppResource {
		meta := map[string]interface{}{
			"csp":         map[string]interface{}{"connectDomains": []string{"https://api.example"}},
			"permissions": map[string]interface{}{"microphone": map[string]interface{}{}},
		}
		if overrideMeta != nil {
			meta = overrideMeta
		}
		return MCPAppResource{
			URI:      "ui://app/dashboard",
			MimeType: MCPAppMimeType,
			HTML:     "<!doctype html><html></html>",
			Meta:     meta,
		}
	}

	t.Run("stable digest for equal resources", func(t *testing.T) {
		if FingerprintMCPAppResource(base(nil)) != FingerprintMCPAppResource(base(nil)) {
			t.Fatal("expected identical fingerprints for structurally equal resources")
		}
	})

	t.Run("ignores key ordering in csp / permissions", func(t *testing.T) {
		a := FingerprintMCPAppResource(base(map[string]interface{}{
			"csp":         map[string]interface{}{"connectDomains": []string{"https://api.example"}, "frameDomains": []string{}},
			"permissions": map[string]interface{}{"microphone": map[string]interface{}{}, "camera": map[string]interface{}{}},
		}))
		b := FingerprintMCPAppResource(base(map[string]interface{}{
			"permissions": map[string]interface{}{"camera": map[string]interface{}{}, "microphone": map[string]interface{}{}},
			"csp":         map[string]interface{}{"frameDomains": []string{}, "connectDomains": []string{"https://api.example"}},
		}))
		if a != b {
			t.Fatalf("fingerprints differ despite only key ordering differing: %s vs %s", a, b)
		}
	})

	t.Run("changes when html, csp, or permissions mutate", func(t *testing.T) {
		baseline := FingerprintMCPAppResource(base(nil))

		withDifferentHTML := base(nil)
		withDifferentHTML.HTML = "<html>evil</html>"
		if FingerprintMCPAppResource(withDifferentHTML) == baseline {
			t.Fatal("expected fingerprint to change when html mutates")
		}

		withDifferentCSP := base(map[string]interface{}{
			"csp": map[string]interface{}{"connectDomains": []string{"https://evil.example"}},
		})
		if FingerprintMCPAppResource(withDifferentCSP) == baseline {
			t.Fatal("expected fingerprint to change when csp mutates")
		}

		withDifferentPermissions := base(map[string]interface{}{
			"permissions": map[string]interface{}{"camera": map[string]interface{}{}},
		})
		if FingerprintMCPAppResource(withDifferentPermissions) == baseline {
			t.Fatal("expected fingerprint to change when permissions mutate")
		}
	})
}

func TestDetectMCPAppResourceDrift(t *testing.T) {
	if !DetectMCPAppResourceDrift("a", "b") {
		t.Fatal("expected drift between different fingerprints")
	}
	if DetectMCPAppResourceDrift("a", "a") {
		t.Fatal("expected no drift between identical fingerprints")
	}
}
