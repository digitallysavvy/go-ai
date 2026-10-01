package fileutil

import "testing"

func TestInferMediaTypeFromURL(t *testing.T) {
	if got := InferMediaTypeFromURL("https://example.com/a.png", "application/octet-stream"); got != "image/png" {
		t.Fatalf("media type = %q, want image/png", got)
	}
	if got := InferMediaTypeFromURL("https://example.com/a", "application/octet-stream"); got != "application/octet-stream" {
		t.Fatalf("fallback media type = %q", got)
	}
	if got := InferMediaTypeFromURL("not-a-url", "application/octet-stream"); got != "application/octet-stream" {
		t.Fatalf("invalid URL fallback = %q", got)
	}
	if got := InferMediaTypeFromURL("relative.png", "application/octet-stream"); got != "application/octet-stream" {
		t.Fatalf("relative URL fallback = %q", got)
	}
	if got := InferMediaTypeFromURL("https://example.com/a.png/", "application/octet-stream"); got != "application/octet-stream" {
		t.Fatalf("trailing slash fallback = %q", got)
	}
	if got := InferMediaTypeFromURL("https://example.com/file%2Epng", "application/octet-stream"); got != "application/octet-stream" {
		t.Fatalf("percent-encoded dot fallback = %q", got)
	}
	if got := InferMediaTypeFromURL("https://example.com/file%zz.png", "application/octet-stream"); got != "image/png" {
		t.Fatalf("invalid percent escape media type = %q, want image/png", got)
	}
	if got := InferMediaTypeFromURL("https:example.com/file.png", "application/octet-stream"); got != "image/png" {
		t.Fatalf("special-scheme URL media type = %q, want image/png", got)
	}
	if got := InferMediaTypeFromURL("https://exa%mple.com/file.png", "application/octet-stream"); got != "application/octet-stream" {
		t.Fatalf("invalid authority fallback = %q", got)
	}
	if got := InferMediaTypeFromURL("https:exa%mple.com/file.png", "application/octet-stream"); got != "application/octet-stream" {
		t.Fatalf("invalid special-scheme authority fallback = %q", got)
	}
	if got := InferMediaTypeFromURL("data:text/plain;base64,abc.png", "application/octet-stream"); got != "image/png" {
		t.Fatalf("opaque data URL media type = %q, want image/png", got)
	}
	if got := InferMediaTypeFromURL("mailto:user@example.com.png", "application/octet-stream"); got != "image/png" {
		t.Fatalf("opaque mailto URL media type = %q, want image/png", got)
	}
	if got := InferMediaTypeFromURL("custom:thing.png", "application/octet-stream"); got != "image/png" {
		t.Fatalf("opaque custom URL media type = %q, want image/png", got)
	}
	if got := InferMediaTypeFromURL("https://user:pa%zz@example.com/file.png", "application/octet-stream"); got != "image/png" {
		t.Fatalf("invalid userinfo escape media type = %q, want image/png", got)
	}
	if got := InferMediaTypeFromURL("https://example.com:bad/file.png", "application/octet-stream"); got != "application/octet-stream" {
		t.Fatalf("invalid port fallback = %q", got)
	}
	if got := InferMediaTypeFromURL(`https://example.com\file.png`, "application/octet-stream"); got != "image/png" {
		t.Fatalf("special-scheme authority backslash media type = %q, want image/png", got)
	}
	if got := InferMediaTypeFromURL(`https:\example.com\file.png`, "application/octet-stream"); got != "image/png" {
		t.Fatalf("special-scheme opaque backslash media type = %q, want image/png", got)
	}
	if got := InferMediaTypeFromURL("https:/example.com/file.png", "application/octet-stream"); got != "image/png" {
		t.Fatalf("special-scheme single-slash media type = %q, want image/png", got)
	}
	if got := InferMediaTypeFromURL(`custom://host\file.png`, "application/octet-stream"); got != "application/octet-stream" {
		t.Fatalf("non-special invalid backslash fallback = %q", got)
	}
}

func TestInferMediaTypeFromURL_PrototypeLikeExtensionCollision(t *testing.T) {
	// Regression parity with TS `.constructor` case:
	// extension lookups must not resolve inherited/prototype properties.
	got := InferMediaTypeFromURL("https://example.com/foo.constructor", "application/octet-stream")
	if got != "application/octet-stream" {
		t.Fatalf("media type = %q, want fallback", got)
	}
}
