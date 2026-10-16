package mcp

import (
	"context"
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
		if FingerprintMCPAppResource(base(nil)) != FingerprintMCPAppResource(base(nil)) { //nolint:staticcheck // deliberately comparing two separate base(nil) calls to verify the fingerprint is stable across equal-but-distinct resource values
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

// TS parseVisibility keeps an array even when filtering empties it, so a tool
// whose visibility lists no known audience is hidden from both the model and
// the app; a non-array visibility is ignored and the tool stays model-visible.
func TestSplitMCPAppToolsEmptyVisibilityHidesTool(t *testing.T) {
	mk := func(name string, visibility interface{}) MCPTool {
		return MCPTool{Name: name, Meta: map[string]interface{}{"ui": map[string]interface{}{
			"resourceUri": "ui://x/" + name,
			"visibility":  visibility,
		}}}
	}
	definitions := ListToolsResult{Tools: []MCPTool{
		mk("empty", []interface{}{}),
		mk("unknown", []interface{}{"other"}),
		mk("notArray", "app"),
		mk("model", []interface{}{"model"}),
	}}
	modelVisible, appVisible, err := SplitMCPAppTools(definitions)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tool := range modelVisible.Tools {
		got = append(got, tool.Name)
	}
	if !reflect.DeepEqual(got, []string{"notArray", "model"}) {
		t.Fatalf("model visible = %v", got)
	}
	if len(appVisible.Tools) != 0 {
		t.Fatalf("app visible = %v", appVisible.Tools)
	}
	meta, err := GetMCPAppToolMeta(mk("unknown", []interface{}{"other"}))
	if err != nil || meta == nil || meta.Visibility == nil || len(meta.Visibility) != 0 {
		t.Fatalf("meta = %#v err=%v", meta, err)
	}
	encoded, _ := json.Marshal(meta)
	if string(encoded) != `{"resourceUri":"ui://x/unknown","visibility":[]}` {
		t.Fatalf("encoded = %s", encoded)
	}
}

// TS `uiMeta?.resourceUri ?? tool._meta?.['ui/resourceUri']`: a null
// ui.resourceUri falls back to the legacy key, and with no legacy key the tool
// simply has no app resource.
func TestGetMCPAppToolMetaNullResourceURIFallsBack(t *testing.T) {
	uri, err := GetMCPAppResourceURI(MCPTool{Meta: map[string]interface{}{
		"ui":                           map[string]interface{}{"resourceUri": nil},
		MCPAppLegacyResourceURIMetaKey: "ui://legacy/app",
	}})
	if err != nil || uri != "ui://legacy/app" {
		t.Fatalf("uri = %q err=%v", uri, err)
	}
	uri, err = GetMCPAppResourceURI(MCPTool{Meta: map[string]interface{}{
		"ui": map[string]interface{}{"resourceUri": nil},
	}})
	if err != nil || uri != "" {
		t.Fatalf("uri = %q err=%v, want no resource and no error", uri, err)
	}
}

// TS getMCPAppResourceFromReadResult: a string text wins even when empty;
// the blob is only used when text is not a string.
func TestGetMCPAppResourceEmptyTextWinsOverBlob(t *testing.T) {
	var res ReadResourceResult
	body := `{"contents":[{"uri":"ui://a","mimeType":"text/html;profile=mcp-app","text":"","blob":"PGI+"}]}`
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatal(err)
	}
	got, err := GetMCPAppResourceFromReadResult("ui://a", res)
	if err != nil || got.HTML != "" {
		t.Fatalf("got %+v, %v; want empty html", got, err)
	}
	body = `{"contents":[{"uri":"ui://a","mimeType":"text/html;profile=mcp-app","blob":"PGI-"}]}`
	res = ReadResourceResult{}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatal(err)
	}
	got, err = GetMCPAppResourceFromReadResult("ui://a", res)
	if err != nil || got.HTML != "<b>" {
		t.Fatalf("got %+v, %v; want blob decoded (url-safe, unpadded)", got, err)
	}
	out, _ := json.Marshal(res.Contents[0])
	if string(out) != `{"uri":"ui://a","mimeType":"text/html;profile=mcp-app","blob":"PGI-"}` {
		t.Fatalf("marshal %s", out)
	}
}

func TestMCPAppResourceErrorTextAndEmptyMeta(t *testing.T) {
	_, err := GetMCPAppResourceFromReadResult("ui://a", ReadResourceResult{Contents: []ResourceContent{{URI: "ui://a", MimeType: "text/plain", Text: "x"}}})
	if err == nil || err.Error() != "Unsupported MCP App resource MIME type: text/plain" {
		t.Fatalf("err = %v", err)
	}
	_, err = ReadMCPAppResource(context.Background(), nil, "http://x")
	if err == nil || err.Error() != "Unsupported MCP App resource URI: http://x" {
		t.Fatalf("err = %v", err)
	}
	b, _ := json.Marshal(MCPAppResource{URI: "u", MimeType: "m", HTML: "h", Meta: map[string]interface{}{}})
	if string(b) != `{"uri":"u","mimeType":"m","html":"h","meta":{}}` {
		t.Fatalf("json = %s", b)
	}
	b, _ = json.Marshal(MCPAppResource{URI: "u", MimeType: "m", HTML: "h"})
	if string(b) != `{"uri":"u","mimeType":"m","html":"h"}` {
		t.Fatalf("json = %s", b)
	}
}
