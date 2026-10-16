package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// FingerprintMCPAppResource returns a stable SHA-256 digest (base64url,
// unpadded) over an MCP App resource's html, csp, and permissions, matching
// TS fingerprintMCPAppResource (mcp-app-fingerprint.ts, hash 48e7e78).
//
// Capture a baseline on first load and compare later reads with
// DetectMCPAppResourceDrift to detect a changed resource. Baseline storage
// and the response to a detected drift are the host's concern.
func FingerprintMCPAppResource(resource MCPAppResource) string {
	var csp interface{}
	var permissions interface{}
	if resource.Meta != nil {
		csp = resource.Meta["csp"]
		permissions = resource.Meta["permissions"]
	}
	if cspMap, ok := csp.(map[string]interface{}); ok && len(resource.cspUndefined) > 0 {
		// A malformed csp field stays on the Zod-parsed object as undefined,
		// which canonicalJSON hashes as null.
		withUndefined := copyMap(cspMap)
		for _, key := range resource.cspUndefined {
			if _, kept := withUndefined[key]; !kept {
				withUndefined[key] = nil
			}
		}
		csp = withUndefined
	}
	canonical := canonicalMCPAppJSON(map[string]interface{}{
		"html":        resource.HTML,
		"csp":         csp,
		"permissions": permissions,
	})
	sum := sha256.Sum256([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// DetectMCPAppResourceDrift compares two fingerprints from
// FingerprintMCPAppResource. Returns true when current differs from
// baseline, matching TS detectMCPAppResourceDrift.
func DetectMCPAppResourceDrift(current, baseline string) bool {
	return current != baseline
}

// canonicalMCPAppJSON produces a deterministic JSON serialization with object
// keys sorted, mirroring TS canonicalJSON (mcp-app-fingerprint.ts /
// util/canonical-hash.ts) so structurally-equal values hash identically
// regardless of key insertion order. The value is first round-tripped
// through JSON so both hand-built Go values (e.g. map[string]interface{} with
// []string) and already-JSON-decoded values (map[string]interface{} with
// []interface{}) normalize the same way.
func canonicalMCPAppJSON(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte("null")
	}
	var normalized interface{}
	if err := json.Unmarshal(data, &normalized); err != nil {
		normalized = nil
	}
	return canonicalizeNormalizedJSON(normalized)
}

func canonicalizeNormalizedJSON(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		// JS sorts keys by UTF-16 code unit, which orders astral characters
		// before U+E000..U+FFFF; byte order does not.
		slices.SortFunc(keys, func(a, b string) int {
			return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
		})
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			keyJSON, _ := jsonstr.Marshal(k)
			parts = append(parts, string(keyJSON)+":"+canonicalizeNormalizedJSON(v[k]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, canonicalizeNormalizedJSON(item))
		}
		return "[" + strings.Join(parts, ",") + "]"
	default:
		// JSON.stringify of a scalar: no HTML escaping of <, > and & (the
		// digest covers the resource's html), U+2028/2029 written as is.
		data, err := jsonstr.Marshal(v)
		if err != nil {
			return "null"
		}
		return string(data)
	}
}
