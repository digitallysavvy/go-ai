package fx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/fx/internal/libfx"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
	"github.com/digitallysavvy/go-ai/pkg/mcp"
)

// Port of TS fx-mcp.ts with libfx's createMcpAdapter (libfx mcp.js,
// Apache-2.0): HTTP and SSE MCP servers become host tools of the core.
// Command (stdio) servers are not supported, as in TS.

const (
	maxMCPTools             = 64
	maxMCPInstructionsBytes = 64 << 10
	maxMCPCursorLength      = 4096
	maxMCPToolNameLength    = 256
	maxFxToolNameLength     = 64
)

// fxMCPServerConfig is one parsed mcpServers entry (TS FxMcpServerConfig).
type fxMCPServerConfig struct {
	URL     string
	Type    string
	Headers map[string]string
}

// mcpSession is the part of an MCP client the adapter uses; tests replace
// it.
type mcpSession interface {
	listTools(ctx context.Context, cursor string) (tools []mcp.MCPTool, nextCursor string, err error)
	callTool(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error)
	instructions() string
	close() error
}

type mcpClientSession struct{ c *mcp.MCPClient }

func (s mcpClientSession) listTools(ctx context.Context, cursor string) ([]mcp.MCPTool, string, error) {
	r, err := s.c.ListToolsWithCursor(ctx, cursor)
	if err != nil {
		return nil, "", err
	}
	return r.Tools, r.NextCursor, nil
}

func (s mcpClientSession) callTool(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	return s.c.CallTool(ctx, name, args)
}

func (s mcpClientSession) instructions() string { return s.c.ServerInstructions() }
func (s mcpClientSession) close() error         { return s.c.Close() }

// connectMCP creates and initializes an MCP client (TS createMCPClient).
var connectMCP = func(ctx context.Context, cfg fxMCPServerConfig) (mcpSession, error) {
	var transport mcp.Transport
	if cfg.Type == "sse" {
		transport = mcp.NewSSETransport(mcp.SSETransportConfig{URL: cfg.URL, Config: mcp.TransportConfig{Headers: cfg.Headers}})
	} else {
		transport = mcp.NewHTTPTransport(mcp.HTTPTransportConfig{URL: cfg.URL, Config: mcp.TransportConfig{Headers: cfg.Headers}})
	}
	client := mcp.NewMCPClient(transport, mcp.MCPClientConfig{})
	if err := client.Connect(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return mcpClientSession{client}, nil
}

// jsonTypeName is zod's name for a received JSON value.
func jsonTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case float64, json.Number:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	default:
		return "object"
	}
}

// parseFxMCPServerConfig mirrors TS parseFxMcpServerConfig: nil for a
// disabled entry.
func parseFxMCPServerConfig(serverName string, raw any) (*fxMCPServerConfig, error) {
	unsupported := func() error {
		return harness.NewCapabilityUnsupportedError(fmt.Sprintf("fx MCP server %s must use an HTTP or SSE URL. Command/stdio servers are not supported.", serverName), HarnessID, nil)
	}
	invalid := func(field, message string) error {
		return fmt.Errorf("Invalid fx MCP server %s field %s: %s", serverName, field, message) //nolint:staticcheck // matches TS SDK's exact error text
	}
	// Settings are Go values; read them as the JSON they stand for.
	var config any
	if b, err := json.Marshal(raw); err != nil || json.Unmarshal(b, &config) != nil {
		return nil, unsupported()
	}
	obj, ok := config.(map[string]any)
	if !ok {
		return nil, unsupported()
	}
	if obj["disabled"] == true {
		return nil, nil
	}
	url, ok := obj["url"].(string)
	if !ok {
		return nil, unsupported()
	}
	out := &fxMCPServerConfig{URL: url}
	if t, present := obj["type"]; present {
		s, _ := t.(string)
		switch s {
		case "http", "sse", "streamableHttp":
			out.Type = s
		default:
			return nil, unsupported()
		}
	}
	if h, present := obj["headers"]; present {
		headers, ok := h.(map[string]any)
		if !ok {
			return nil, invalid("headers", "Invalid input: expected record, received "+jsonTypeName(h))
		}
		keys := make([]string, 0, len(headers))
		for k := range headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out.Headers = map[string]string{}
		for _, k := range keys {
			v, ok := headers[k].(string)
			if !ok {
				return nil, invalid("headers."+k, "Invalid input: expected string, received "+jsonTypeName(headers[k]))
			}
			out.Headers[k] = v
		}
	}
	if d, present := obj["disabled"]; present {
		if _, ok := d.(bool); !ok {
			return nil, invalid("disabled", "Invalid input: expected boolean, received "+jsonTypeName(d))
		}
	}
	return out, nil
}

// fxMCPTool is one adapted MCP tool.
type fxMCPTool struct {
	name        string
	description string
	inputSchema any
	execute     func(ctx context.Context, input json.RawMessage) (any, error)
}

// fxMCPRuntime is TS FxMcpRuntime.
type fxMCPRuntime struct {
	tools        []fxMCPTool
	instructions string
	sessions     []mcpSession
}

func (r *fxMCPRuntime) close() error {
	if r == nil {
		return nil
	}
	var errs []error
	for _, s := range r.sessions {
		if err := s.close(); err != nil {
			errs = append(errs, err)
		}
	}
	r.sessions = nil
	return errors.Join(errs...)
}

// createFxMCPRuntime mirrors TS createFxMcpRuntime. Servers are taken in
// name order (a Go map has no insertion order). A failure closes every
// client opened so far.
func createFxMCPRuntime(ctx context.Context, servers map[string]any) (*fxMCPRuntime, error) {
	rt := &fxMCPRuntime{}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	var instructions []string
	for _, name := range names {
		config, err := parseFxMCPServerConfig(name, servers[name])
		if err != nil {
			_ = rt.close()
			return nil, err
		}
		if config == nil {
			continue
		}
		session, err := connectMCP(ctx, *config)
		if err != nil {
			_ = rt.close()
			return nil, err
		}
		rt.sessions = append(rt.sessions, session)
		if text := session.instructions(); text != "" {
			instructions = append(instructions, text)
		}
		tools, err := createMCPAdapter(ctx, session, "mcp_"+sanitizeToolName(name)+"_")
		if err != nil {
			_ = rt.close()
			return nil, err
		}
		rt.tools = append(rt.tools, tools...)
	}
	rt.instructions = strings.Join(instructions, "\n\n")
	return rt, nil
}

// sanitizeToolName replaces every UTF-16 unit outside [A-Za-z0-9_-] with
// "_" (JS .replace(/[^A-Za-z0-9_-]/g, "_")).
func sanitizeToolName(s string) string {
	var b strings.Builder
	for _, u := range utf16.Encode([]rune(s)) {
		if u < 128 && (u == '_' || u == '-' || (u >= '0' && u <= '9') || (u >= 'a' && u <= 'z') || (u >= 'A' && u <= 'Z')) {
			b.WriteByte(byte(u))
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// createMCPAdapter mirrors libfx createMcpAdapter: it lists the server's
// tools (bounded pagination) and adapts each to a host tool.
func createMCPAdapter(ctx context.Context, session mcpSession, prefix string) ([]fxMCPTool, error) {
	var catalog []mcp.MCPTool
	seen := map[string]bool{}
	cursor := ""
	for page := 0; ; page++ {
		if page >= maxMCPTools {
			return nil, errors.New("MCP tool pagination exceeded its page limit")
		}
		tools, next, err := session.listTools(ctx, cursor)
		if err != nil {
			return nil, err
		}
		if len(tools) > maxMCPTools-len(catalog) {
			return nil, errors.New("MCP listTools() returned an invalid tool catalog")
		}
		catalog = append(catalog, tools...)
		if next == "" {
			break
		}
		if len(next) > maxMCPCursorLength || seen[next] {
			return nil, errors.New("MCP listTools() returned an invalid pagination cursor")
		}
		seen[next] = true
		cursor = next
	}
	names := map[string]bool{}
	listed := map[string]bool{}
	out := make([]fxMCPTool, 0, len(catalog))
	for index, tool := range catalog {
		if tool.Name == "" || len(utf16.Encode([]rune(tool.Name))) > maxMCPToolNameLength {
			return nil, fmt.Errorf("MCP tool %d is invalid", index)
		}
		if listed[tool.Name] {
			return nil, fmt.Errorf("MCP tool %s is duplicated", tool.Name)
		}
		listed[tool.Name] = true
		base := sanitizeToolName(prefix + tool.Name)
		name := truncate(base, maxFxToolNameLength)
		for suffix := 2; names[name]; suffix++ {
			tail := fmt.Sprintf("_%d", suffix)
			name = truncate(base, maxFxToolNameLength-len(tail)) + tail
		}
		names[name] = true
		description := tool.Description
		if description == "" {
			description = "MCP tool"
		}
		var schema any = map[string]any{"type": "object", "properties": map[string]any{}}
		if tool.InputSchema != nil {
			schema = tool.InputSchema
		}
		remote := tool.Name
		out = append(out, fxMCPTool{
			name:        name,
			description: description,
			inputSchema: schema,
			execute: func(ctx context.Context, input json.RawMessage) (any, error) {
				var args map[string]any
				if len(input) > 0 && string(input) != "null" {
					if err := json.Unmarshal(input, &args); err != nil {
						return nil, err
					}
				}
				result, err := session.callTool(ctx, remote, args)
				if err != nil {
					return nil, err
				}
				return mcpToolOutput(remote, result)
			},
		})
	}
	return out, nil
}

// mcpContentText mirrors libfx contentText in tool mode.
func mcpContentText(content []mcp.ToolResultContent) string {
	var parts []string
	for _, item := range content {
		text := ""
		switch {
		case item.Type == "text" && item.Text != "":
			text = item.Text
		case item.Type == "resource" && item.Resource != nil && item.Resource.Text != "":
			text = item.Resource.Text
		case item.Text != "":
			text = item.Text
		case item.Type == "resource_link" && item.URI != "":
			name := item.Name
			if name == "" {
				name = "Resource"
			}
			text = name + ": " + item.URI
		}
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

// mcpResultText mirrors libfx resultText.
func mcpResultText(result *mcp.CallToolResult) string {
	text := mcpContentText(result.Content)
	if result.StructuredContent != nil {
		b, _ := jsstr.Marshal(result.StructuredContent)
		structured := string(b)
		if text != "" && text != structured {
			return text + "\n" + structured
		}
		return structured
	}
	return text
}

// mcpToolOutput turns a tools/call result into a host tool result: text,
// or a rich result when it carries images; an isError result fails the
// call.
func mcpToolOutput(name string, result *mcp.CallToolResult) (any, error) {
	if result == nil {
		result = &mcp.CallToolResult{}
	}
	text := mcpResultText(result)
	var images []libfx.ToolImage
	for _, item := range result.Content {
		switch {
		case item.Type == "image":
			images = append(images, libfx.ToolImage{Type: "image", MimeType: item.MimeType, Data: item.Data})
		case item.Type == "resource" && item.Resource != nil && strings.HasPrefix(item.Resource.MimeType, "image/") && item.Resource.Blob != "":
			images = append(images, libfx.ToolImage{Type: "image", MimeType: item.Resource.MimeType, Data: item.Resource.Blob})
		}
	}
	var rich *libfx.TypedToolResult
	if len(images) > 0 {
		rich = &libfx.TypedToolResult{Type: "libfx.tool-result", Text: text, Images: images}
	}
	if result.IsError {
		message := text
		if message == "" {
			message = fmt.Sprintf("MCP tool %s failed", name)
		}
		if rich != nil {
			t := true
			r := *rich
			r.IsError = &t
			return nil, &libfx.ToolResultError{Result: r}
		}
		return nil, errors.New(message)
	}
	if rich != nil {
		return rich, nil
	}
	return text, nil
}
