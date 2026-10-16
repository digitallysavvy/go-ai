package mcp

import "testing"

// TS getMCPAppToolMeta throws `Invalid MCP App resource URI: ${JSON.stringify(uri)}`;
// JSON.stringify leaves <, > and & unescaped.
func TestR30InvalidAppResourceURIMessageNotHTMLEscaped(t *testing.T) {
	_, err := GetMCPAppToolMeta(MCPTool{Name: "t", Meta: map[string]interface{}{
		"ui": map[string]interface{}{"resourceUri": "http://x/?a=1&b=<2>"},
	}})
	if err == nil {
		t.Fatal("expected error")
	}
	want := `Invalid MCP App resource URI: "http://x/?a=1&b=<2>"`
	if err.Error() != want {
		t.Fatalf("got %q want %q", err.Error(), want)
	}
}

// TS builds the protected-resource metadata URL from the WHATWG pathname,
// which keeps percent-encoding.
func TestR30ProtectedResourceMetadataURLKeepsEncodedPath(t *testing.T) {
	got, err := protectedResourceMetadataURL("https://h.example/a%2Fb/?x=1", "")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://h.example/.well-known/oauth-protected-resource/a%2Fb?x=1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
