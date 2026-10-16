package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

const (
	MCPAppExtensionName            = "io.modelcontextprotocol/ui"
	MCPAppMimeType                 = "text/html;profile=mcp-app"
	MCPAppLegacyResourceURIMetaKey = "ui/resourceUri"
)

// MCPAppToolMeta is the normalized _meta.ui metadata from an MCP tool.
type MCPAppToolMeta struct {
	ResourceURI string
	Visibility  []string
	Extra       map[string]interface{}
}

// MarshalJSON emits the TypeScript MCPAppToolMeta shape with resourceUri,
// visibility, and all extra _meta.ui keys at the top level.
func (m MCPAppToolMeta) MarshalJSON() ([]byte, error) {
	out := copyMap(m.Extra)
	if m.ResourceURI != "" {
		out["resourceUri"] = m.ResourceURI
	}
	if m.Visibility != nil {
		out["visibility"] = m.Visibility
	}
	return json.Marshal(out)
}

// MCPAppResource is the normalized HTML resource a host can render.
type MCPAppResource struct {
	URI      string                 `json:"uri"`
	MimeType string                 `json:"mimeType"`
	HTML     string                 `json:"html"`
	Meta     map[string]interface{} `json:"meta,omitempty"`

	// cspUndefined lists csp fields present but malformed in the read
	// result. Zod's catch(undefined) keeps such a key (as undefined) on the
	// parsed csp, which TS canonicalJSON hashes as null, so the fingerprint
	// needs to know about keys Meta no longer carries.
	cspUndefined []string
}

// MarshalJSON keeps an empty-but-present meta object (`"meta":{}`), as
// JSON.stringify does for the TS resource; only a nil Meta is omitted.
func (r MCPAppResource) MarshalJSON() ([]byte, error) {
	if r.Meta != nil && len(r.Meta) == 0 {
		return json.Marshal(struct {
			URI      string                 `json:"uri"`
			MimeType string                 `json:"mimeType"`
			HTML     string                 `json:"html"`
			Meta     map[string]interface{} `json:"meta"`
		}{r.URI, r.MimeType, r.HTML, r.Meta})
	}
	type plain MCPAppResource
	return json.Marshal(plain(r))
}

// MCPAppClientCapabilities returns the client capabilities advertised by hosts
// that support MCP Apps.
func MCPAppClientCapabilities() ClientCapabilities {
	return ClientCapabilities{
		Extensions: map[string]interface{}{
			MCPAppExtensionName: map[string]interface{}{
				"mimeTypes": []string{MCPAppMimeType},
			},
		},
	}
}

// GetMCPAppToolMeta reads and validates MCP Apps metadata from a tool
// definition. It accepts both the current _meta.ui.resourceUri shape and the
// legacy flat _meta["ui/resourceUri"] key.
func GetMCPAppToolMeta(tool MCPTool) (*MCPAppToolMeta, error) {
	uiMeta := mapFromInterface(tool.Meta["ui"])
	// TS: `uiMeta?.resourceUri ?? tool._meta?.['ui/resourceUri']`; a null
	// ui.resourceUri falls through to the legacy key like a missing one.
	resourceValue, hasResource := uiMeta["resourceUri"]
	if resourceValue == nil {
		resourceValue, hasResource = tool.Meta[MCPAppLegacyResourceURIMetaKey]
	}
	if !hasResource && uiMeta == nil {
		return nil, nil
	}

	meta := &MCPAppToolMeta{Extra: copyMap(uiMeta)}
	if hasResource {
		resourceURI, ok := resourceValue.(string)
		if !ok || !strings.HasPrefix(resourceURI, "ui://") {
			// JSON.stringify does not HTML-escape <, > or &.
			encoded, _ := jsonstr.Marshal(resourceValue)
			return nil, mcpAppError("Invalid MCP App resource URI: %s", encoded)
		}
		meta.ResourceURI = resourceURI
		meta.Extra["resourceUri"] = resourceURI
	}
	// TS parseVisibility: an array is filtered to "model"/"app" and replaces
	// the raw value even when the result is empty (an empty visibility list
	// hides the tool from both the model and the app); a non-array leaves the
	// visibility unset.
	if visibilityValue, ok := uiMeta["visibility"]; ok {
		if meta.Visibility = parseMCPAppVisibility(visibilityValue); meta.Visibility != nil {
			meta.Extra["visibility"] = meta.Visibility
		}
	}
	return meta, nil
}

// GetMCPAppResourceURI returns the ui:// app resource URI attached to a tool.
func GetMCPAppResourceURI(tool MCPTool) (string, error) {
	meta, err := GetMCPAppToolMeta(tool)
	if err != nil || meta == nil {
		return "", err
	}
	return meta.ResourceURI, nil
}

// IsMCPAppTool reports whether a tool has an MCP App resource attached.
func IsMCPAppTool(tool MCPTool) (bool, error) {
	uri, err := GetMCPAppResourceURI(tool)
	return uri != "", err
}

// SplitMCPAppTools splits tool definitions into model-visible and app-visible
// lists while preserving pagination metadata.
func SplitMCPAppTools(definitions ListToolsResult) (modelVisible ListToolsResult, appVisible ListToolsResult, err error) {
	// TS always returns a tools array; keep an empty one empty (not null).
	modelVisible.Tools, appVisible.Tools = []MCPTool{}, []MCPTool{}
	modelVisible.NextCursor = definitions.NextCursor
	appVisible.NextCursor = definitions.NextCursor
	for _, tool := range definitions.Tools {
		meta, metaErr := GetMCPAppToolMeta(tool)
		if metaErr != nil {
			return ListToolsResult{}, ListToolsResult{}, metaErr
		}
		if meta == nil || meta.Visibility == nil || containsString(meta.Visibility, "model") {
			modelVisible.Tools = append(modelVisible.Tools, tool)
		}
		if meta != nil && containsString(meta.Visibility, "app") {
			appVisible.Tools = append(appVisible.Tools, tool)
		}
	}
	return modelVisible, appVisible, nil
}

// GetMCPAppResourceURIs returns unique ui:// resource URIs referenced by tools.
func GetMCPAppResourceURIs(definitions ListToolsResult) ([]string, error) {
	seen := map[string]bool{}
	uris := []string{}
	for _, tool := range definitions.Tools {
		uri, err := GetMCPAppResourceURI(tool)
		if err != nil {
			return nil, err
		}
		if uri != "" && !seen[uri] {
			seen[uri] = true
			uris = append(uris, uri)
		}
	}
	return uris, nil
}

// GetMCPAppResourceFromReadResult extracts app HTML and rendering metadata
// from a resources/read result.
func GetMCPAppResourceFromReadResult(uri string, resource ReadResourceResult) (MCPAppResource, error) {
	for _, content := range resource.Contents {
		if content.URI != uri {
			continue
		}
		if content.MimeType != MCPAppMimeType {
			// A missing mimeType reads as "undefined" in the TS template string.
			mime := content.MimeType
			if mime == "" {
				mime = "undefined"
			}
			return MCPAppResource{}, mcpAppError("Unsupported MCP App resource MIME type: %s", mime)
		}
		// TS: a string text wins (even ""), else a string blob is decoded.
		var html string
		switch {
		case content.Text != "" || content.textPresent:
			html = content.Text
		case content.Blob != "" || content.blobPresent:
			decoded, err := decodeBase64Lenient(content.Blob)
			if err != nil {
				// TS: atob throws on malformed base64 before any format check.
				return MCPAppResource{}, mcpAppError("The string to be decoded is not correctly encoded.")
			}
			html = decodeUTF8Lenient(decoded)
		default:
			return MCPAppResource{}, mcpAppError("Unsupported MCP App resource content format: %s", uri)
		}
		var meta map[string]interface{}
		var cspUndefined []string
		if uiMeta := mapFromInterface(content.Meta["ui"]); uiMeta != nil {
			meta, cspUndefined = validateMCPAppResourceMeta(uiMeta)
		}
		return MCPAppResource{
			URI:      uri,
			MimeType: MCPAppMimeType,
			HTML:     html,
			Meta:     meta,

			cspUndefined: cspUndefined,
		}, nil
	}
	return MCPAppResource{}, fmt.Errorf("MCP App resource not found in read result: %s", uri)
}

// ReadMCPAppResource reads and normalizes a ui:// MCP App resource.
func ReadMCPAppResource(ctx context.Context, client *MCPClient, uri string) (MCPAppResource, error) {
	if !strings.HasPrefix(uri, "ui://") {
		return MCPAppResource{}, mcpAppError("Unsupported MCP App resource URI: %s", uri)
	}
	result, err := client.ReadResource(ctx, uri)
	if err != nil {
		return MCPAppResource{}, err
	}
	return GetMCPAppResourceFromReadResult(uri, *result)
}

// validateMCPAppResourceCSP mirrors TS MCPAppResourceCSPSchema (a loose
// zod object, mcp-apps.ts, hash 48e7e78): connectDomains/resourceDomains/
// frameDomains are each filtered to their string elements when present as an
// array, and dropped entirely when the field is present but not an array.
// Unknown keys pass through unchanged (forward-compat).
func validateMCPAppResourceCSP(raw map[string]interface{}) (map[string]interface{}, []string) {
	out := copyMap(raw)
	var dropped []string
	for _, key := range []string{"connectDomains", "resourceDomains", "frameDomains"} {
		value, ok := out[key]
		if !ok {
			continue
		}
		values, ok := value.([]interface{})
		if !ok {
			delete(out, key)
			dropped = append(dropped, key)
			continue
		}
		strs := make([]string, 0, len(values))
		for _, v := range values {
			if s, ok := v.(string); ok {
				strs = append(strs, s)
			}
		}
		out[key] = strs
	}
	return out, dropped
}

// validateMCPAppResourceMeta mirrors TS MCPAppResourceMetaSchema (a loose
// zod object with `.catch(undefined)` per field, mcp-apps.ts, hash 48e7e78):
// prefersBorder must be a bool, csp must be an object (validated
// recursively), and permissions must be an object; a malformed field is
// dropped rather than failing the whole parse. Unknown keys pass through
// unchanged for forward-compat.
func validateMCPAppResourceMeta(raw map[string]interface{}) (map[string]interface{}, []string) {
	out := copyMap(raw)
	var cspDropped []string
	if value, ok := out["prefersBorder"]; ok {
		if _, isBool := value.(bool); !isBool {
			delete(out, "prefersBorder")
		}
	}
	if value, ok := out["csp"]; ok {
		if cspMap, isMap := value.(map[string]interface{}); isMap {
			out["csp"], cspDropped = validateMCPAppResourceCSP(cspMap)
		} else {
			delete(out, "csp")
		}
	}
	if value, ok := out["permissions"]; ok {
		if _, isMap := value.(map[string]interface{}); !isMap {
			delete(out, "permissions")
		}
	}
	return out, cspDropped
}

// parseMCPAppVisibility filters an array to "model"/"app" entries. It returns
// nil when value is not an array and a non-nil (possibly empty) slice
// otherwise.
func parseMCPAppVisibility(value interface{}) []string {
	values, ok := value.([]interface{})
	if !ok {
		stringsValue, isStrings := value.([]string)
		if !isStrings {
			return nil
		}
		values = make([]interface{}, len(stringsValue))
		for i, v := range stringsValue {
			values[i] = v
		}
	}
	out := []string{}
	for _, v := range values {
		s, ok := v.(string)
		if ok && (s == "model" || s == "app") {
			out = append(out, s)
		}
	}
	return out
}

func mapFromInterface(value interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}
	if m, ok := value.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func copyMap(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// decodeBase64Lenient mirrors TS convertBase64ToUint8Array: base64url
// characters are mapped to the standard alphabet and (as with atob) padding
// is optional and ASCII whitespace is ignored.
func decodeBase64Lenient(s string) ([]byte, error) {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '-':
			return '+'
		case '_':
			return '/'
		case ' ', '\t', '\n', '\f', '\r':
			return -1
		}
		return r
	}, s)
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
}

// mcpAppError builds a plain error whose text starts with a capital letter,
// like the TS `new Error(...)` messages (staticcheck ST1005 only inspects
// literal error constructors).
func mcpAppError(format string, args ...interface{}) error {
	msg := fmt.Sprintf(format, args...)
	return errors.New(msg)
}

// decodeUTF8Lenient mirrors `new TextDecoder().decode(bytes)` (WHATWG UTF-8
// decode): a leading BOM is dropped and every maximal invalid subpart becomes
// one U+FFFD, so the html (and its fingerprint) match TS for a blob that is
// not valid UTF-8.
func decodeUTF8Lenient(b []byte) string {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	for i := 0; i < len(b); {
		if r, size := utf8.DecodeRune(b[i:]); r != utf8.RuneError || size > 1 {
			out.Write(b[i : i+size])
			i += size
			continue
		}
		lead := b[i]
		need, lower, upper := 0, byte(0x80), byte(0xBF)
		switch {
		case lead >= 0xC2 && lead <= 0xDF:
			need = 1
		case lead >= 0xE0 && lead <= 0xEF:
			need = 2
			switch lead {
			case 0xE0:
				lower = 0xA0
			case 0xED:
				upper = 0x9F
			}
		case lead >= 0xF0 && lead <= 0xF4:
			need = 3
			switch lead {
			case 0xF0:
				lower = 0x90
			case 0xF4:
				upper = 0x8F
			}
		}
		i++
		for ; need > 0 && i < len(b) && b[i] >= lower && b[i] <= upper; need-- {
			i++
			lower, upper = 0x80, 0xBF
		}
		out.WriteRune(utf8.RuneError)
	}
	return out.String()
}
