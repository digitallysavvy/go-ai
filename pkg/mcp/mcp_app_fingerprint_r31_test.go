package mcp

import "testing"

// Zod's catch(undefined) keeps a malformed csp field as an undefined key on
// the parsed object and TS canonicalJSON hashes it as null, so the digest of
// {connectDomains: "x"} is the digest of {"connectDomains":null}, not of {}.
func TestFingerprintMCPAppResourceMalformedCSPFieldHashesAsNull(t *testing.T) {
	res, err := GetMCPAppResourceFromReadResult("ui://x", ReadResourceResult{Contents: []ResourceContent{{
		URI: "ui://x", MimeType: MCPAppMimeType, Text: "<p>",
		Meta: map[string]interface{}{"ui": map[string]interface{}{
			"csp": map[string]interface{}{"connectDomains": "not-an-array", "frameDomains": []interface{}{"a"}},
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	want := FingerprintMCPAppResource(MCPAppResource{HTML: "<p>", Meta: map[string]interface{}{
		"csp": map[string]interface{}{"connectDomains": nil, "frameDomains": []interface{}{"a"}},
	}})
	if got := FingerprintMCPAppResource(res); got != want {
		t.Fatalf("fingerprint = %s, want %s", got, want)
	}
	absent := FingerprintMCPAppResource(MCPAppResource{HTML: "<p>", Meta: map[string]interface{}{
		"csp": map[string]interface{}{"frameDomains": []interface{}{"a"}},
	}})
	if FingerprintMCPAppResource(res) == absent {
		t.Fatal("a malformed field must not hash like an absent one")
	}
}
