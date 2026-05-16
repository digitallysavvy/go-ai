package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
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
	resourceValue, hasResource := uiMeta["resourceUri"]
	if !hasResource {
		resourceValue, hasResource = tool.Meta[MCPAppLegacyResourceURIMetaKey]
	}
	if !hasResource && uiMeta == nil {
		return nil, nil
	}

	meta := &MCPAppToolMeta{Extra: copyMap(uiMeta)}
	if hasResource {
		resourceURI, ok := resourceValue.(string)
		if !ok || !strings.HasPrefix(resourceURI, "ui://") {
			encoded, _ := json.Marshal(resourceValue)
			return nil, fmt.Errorf("invalid MCP App resource URI: %s", encoded)
		}
		meta.ResourceURI = resourceURI
		meta.Extra["resourceUri"] = resourceURI
	}
	if visibilityValue, ok := uiMeta["visibility"]; ok {
		meta.Visibility = parseMCPAppVisibility(visibilityValue)
		if len(meta.Visibility) > 0 {
			meta.Extra["visibility"] = meta.Visibility
		}
	}
	if len(meta.Extra) == 0 && meta.ResourceURI == "" {
		return nil, nil
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
	modelVisible.NextCursor = definitions.NextCursor
	appVisible.NextCursor = definitions.NextCursor
	for _, tool := range definitions.Tools {
		meta, metaErr := GetMCPAppToolMeta(tool)
		if metaErr != nil {
			return ListToolsResult{}, ListToolsResult{}, metaErr
		}
		if meta == nil || len(meta.Visibility) == 0 || containsString(meta.Visibility, "model") {
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
			return MCPAppResource{}, fmt.Errorf("unsupported MCP App resource MIME type: %s", content.MimeType)
		}
		html := content.Text
		if html == "" && content.Blob != "" {
			decoded, err := base64.StdEncoding.DecodeString(content.Blob)
			if err != nil {
				return MCPAppResource{}, fmt.Errorf("unsupported MCP App resource content format: %s", uri)
			}
			html = string(decoded)
		}
		if html == "" {
			return MCPAppResource{}, fmt.Errorf("unsupported MCP App resource content format: %s", uri)
		}
		return MCPAppResource{
			URI:      uri,
			MimeType: MCPAppMimeType,
			HTML:     html,
			Meta:     mapFromInterface(content.Meta["ui"]),
		}, nil
	}
	return MCPAppResource{}, fmt.Errorf("MCP App resource not found in read result: %s", uri)
}

// ReadMCPAppResource reads and normalizes a ui:// MCP App resource.
func ReadMCPAppResource(ctx context.Context, client *MCPClient, uri string) (MCPAppResource, error) {
	if !strings.HasPrefix(uri, "ui://") {
		return MCPAppResource{}, fmt.Errorf("unsupported MCP App resource URI: %s", uri)
	}
	result, err := client.ReadResource(ctx, uri)
	if err != nil {
		return MCPAppResource{}, err
	}
	return GetMCPAppResourceFromReadResult(uri, *result)
}

func parseMCPAppVisibility(value interface{}) []string {
	values, ok := value.([]interface{})
	if !ok {
		if stringsValue, ok := value.([]string); ok {
			values = make([]interface{}, len(stringsValue))
			for i, v := range stringsValue {
				values[i] = v
			}
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
