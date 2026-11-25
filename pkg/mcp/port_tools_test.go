package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS 95b78c93e7: a POST whose SSE response ends (cleanly or broken) before its
// JSON-RPC response gets a request-specific error so the client rejects.
func TestPortToolsHTTPSSEEndsEarlyRejectsPendingRequest(t *testing.T) {
	for _, mode := range []string{"ends cleanly", "breaks off"} {
		t.Run(mode, func(t *testing.T) {
			release := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, ": keep-alive\n\n")
				w.(http.Flusher).Flush()
				<-release
				if mode == "breaks off" {
					panic(http.ErrAbortHandler)
				}
			}))
			defer srv.Close()
			tr := NewHTTPTransport(HTTPTransportConfig{InitialProtocolVersion: LatestLegacyProtocolVersion, URL: srv.URL, OnError: func(error) {}})
			tr.connected = true
			msg, _ := CreateRequest(7, "tools/call", nil)
			if err := tr.Send(t.Context(), msg); err != nil {
				t.Fatal(err)
			}
			close(release)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var got *MCPMessage
			for ctx.Err() == nil {
				m, err := tr.Receive(ctx)
				if err == nil && m != nil {
					got = m
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if got == nil || got.Error == nil || got.Error.Code != -32000 ||
				!strings.Contains(got.Error.Message, "SSE stream ended before the response for request 7") {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestPortToolsHTTPSSEMatchingResponseSuppressesEndError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{}}\n\n")
	}))
	defer srv.Close()
	tr := NewHTTPTransport(HTTPTransportConfig{InitialProtocolVersion: LatestLegacyProtocolVersion, URL: srv.URL, OnError: func(error) {}})
	tr.connected = true
	msg, _ := CreateRequest(7, "tools/call", nil)
	if err := tr.Send(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	var all []*MCPMessage
	for {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		m, err := tr.Receive(ctx)
		cancel()
		if err != nil || m == nil {
			break
		}
		all = append(all, m)
	}
	if len(all) != 1 || all[0].Error != nil {
		t.Fatalf("messages = %+v", all)
	}
}

// TS d55f3cfc45 (mcp-http-transport.test.ts "preserve a version error"): the
// discovery guidance error keeps the HTTP status and URL of the failed POST.
func TestPortToolsDiscoveryGuidanceErrorKeepsHTTPStatusAndURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32022,"message":"Unsupported protocol version","data":{"requested":"2026-07-28","supported":["2025-11-25"]}}}`)
	}))
	defer srv.Close()
	c := NewMCPClient(NewHTTPTransport(HTTPTransportConfig{URL: srv.URL, OnError: func(error) {}}), MCPClientConfig{})
	err := c.Connect(t.Context())
	defer c.Close() //nolint:errcheck
	var ce *MCPClientError
	if !errors.As(err, &ce) || ce.Code != -32022 || ce.StatusCode != 400 || ce.URL != srv.URL || !strings.Contains(ce.Message, "protocolVersionDiscovery: false") {
		t.Fatalf("err = %#v", err)
	}
}

// TS create-mcp-tools.test.ts (93c63f71a1).

func TestPortToolsCreateMCPToolsNoExecuteNoConnection(t *testing.T) {
	definitions := []MCPTool{{Name: "getCart", Description: "Read the cart", InputSchema: map[string]interface{}{"type": "object"}}}
	tools, err := CreateMCPTools(definitions)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "getCart" || tools[0].Type != types.ToolTypeDynamic || tools[0].Description != "Read the cart" || tools[0].Execute != nil {
		t.Fatalf("tools = %+v", tools)
	}
	want := map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}
	if !reflect.DeepEqual(tools[0].Parameters, want) {
		t.Fatalf("parameters = %#v", tools[0].Parameters)
	}
	if _, has := definitions[0].InputSchema["additionalProperties"]; has {
		t.Fatal("definition input schema was modified")
	}
	empty, err := CreateMCPTools([]MCPTool{})
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty = %v, %v", empty, err)
	}
}

func TestPortToolsCreateMCPToolsPreservesSchemaTitleAnnotationsMeta(t *testing.T) {
	raw := `[{"name":"getCart","title":"Cart","inputSchema":{"type":"object","properties":{"currency":{"type":"string","enum":["USD","EUR"]}},"required":["currency"]},
	"annotations":{"title":"Fallback title","readOnlyHint":true},
	"_meta":{"ai-sdk.dev/webmcp":{"name":"getCart","origin":"https://shop.example"},"ui":{"resourceUri":"ui://cart/view.html"}}}]`
	tools, err := CreateMCPTools(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	tool := tools[0]
	if tool.Title != "Cart" {
		t.Fatalf("title = %q", tool.Title)
	}
	params := tool.Parameters.(map[string]interface{})
	if params["additionalProperties"] != false || params["required"] == nil {
		t.Fatalf("params = %#v", params)
	}
	wantMeta := map[string]interface{}{
		"toolName":    "getCart",
		"title":       "Cart",
		"annotations": map[string]interface{}{"title": "Fallback title", "readOnlyHint": true},
		"app":         map[string]interface{}{"resourceUri": "ui://cart/view.html", "mimeType": "text/html;profile=mcp-app"},
	}
	if !reflect.DeepEqual(tool.Metadata, wantMeta) {
		t.Fatalf("metadata = %#v", tool.Metadata)
	}
	if tool.Meta["ui"] == nil || tool.Meta["ai-sdk.dev/webmcp"] == nil {
		t.Fatalf("meta = %#v", tool.Meta)
	}
	fallback, err := CreateMCPTools([]interface{}{map[string]interface{}{"name": "getCart", "inputSchema": map[string]interface{}{"type": "object"}, "annotations": map[string]interface{}{"title": "Fallback title"}}})
	if err != nil || fallback[0].Title != "Fallback title" {
		t.Fatalf("fallback = %+v, %v", fallback, err)
	}
}

func TestPortToolsCreateMCPToolsConvertsResultsToModelContent(t *testing.T) {
	tools, err := CreateMCPTools([]MCPTool{{Name: "getCart", InputSchema: map[string]interface{}{"type": "object"}}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := tools[0].ToModelOutput(t.Context(), types.ToModelOutputOptions{
		ToolCallID: "call-1",
		Input:      map[string]interface{}{},
		Output: CallToolResult{Content: []ToolResultContent{
			{Type: "text", Text: "Cart total: $75"},
			{Type: "image", Data: "aGVsbG8=", MimeType: "image/png"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != types.ToolResultOutputContent || len(out.Content) != 2 {
		t.Fatalf("out = %+v", out)
	}
	if text, ok := out.Content[0].(types.TextContentBlock); !ok || text.Text != "Cart total: $75" {
		t.Fatalf("block 0 = %#v", out.Content[0])
	}
	if file, ok := out.Content[1].(types.FileContentBlock); !ok || file.FileData.MediaType != "image/png" || file.FileData.DataString != "aGVsbG8=" {
		t.Fatalf("block 1 = %#v", out.Content[1])
	}
}

func TestPortToolsCreateMCPToolsRejectsMalformedDefinitions(t *testing.T) {
	for _, defs := range []interface{}{
		nil,
		map[string]interface{}{},
		[]interface{}{nil},
		[]interface{}{map[string]interface{}{"name": "getCart"}},
		[]interface{}{map[string]interface{}{"name": 42, "inputSchema": map[string]interface{}{}}},
		[]interface{}{map[string]interface{}{"name": "getCart", "inputSchema": "invalid"}},
		[]interface{}{map[string]interface{}{"name": "getCart", "inputSchema": map[string]interface{}{}, "annotations": map[string]interface{}{"readOnlyHint": "yes"}}},
	} {
		tools, err := CreateMCPTools(defs)
		var ce *MCPClientError
		if !errors.As(err, &ce) || ce.Message != "Invalid MCP tool definitions" || ce.Cause == nil || tools != nil {
			t.Errorf("%#v: tools=%v err=%#v", defs, tools, err)
		}
	}
}

func TestPortToolsCreateMCPToolsRejectsDuplicateNames(t *testing.T) {
	schema := map[string]interface{}{}
	_, err := CreateMCPTools([]MCPTool{{Name: "getCart", InputSchema: schema}, {Name: "getCart", InputSchema: schema}})
	if err == nil || err.Error() != `Duplicate MCP tool name "getCart"` {
		t.Fatalf("err = %v", err)
	}
}

func TestPortToolsCreateMCPToolsWrapsInvalidAppResourceURI(t *testing.T) {
	for _, meta := range []map[string]interface{}{
		{"ui": map[string]interface{}{"resourceUri": "https://example.com/cart"}},
		{"ui": map[string]interface{}{"resourceUri": 42}},
		{"ui/resourceUri": "https://example.com/cart"},
		{"ui/resourceUri": 42},
	} {
		_, err := CreateMCPTools([]MCPTool{{Name: "getCart", InputSchema: map[string]interface{}{}, Meta: meta}})
		var ce *MCPClientError
		if !errors.As(err, &ce) || ce.Message != `Invalid MCP tool definition "getCart"` || ce.Cause == nil || !strings.Contains(ce.Cause.Error(), "Invalid MCP App resource URI:") {
			t.Errorf("%v: err = %#v", meta, err)
		}
	}
}

func TestPortToolsCreateMCPToolsKeepsPrototypeNamedTools(t *testing.T) {
	names := []string{"__proto__", "constructor", "toString"}
	defs := make([]MCPTool, len(names))
	for i, n := range names {
		defs[i] = MCPTool{Name: n, InputSchema: map[string]interface{}{"type": "object"}}
	}
	tools, err := CreateMCPTools(defs)
	if err != nil || len(tools) != 3 {
		t.Fatalf("tools=%v err=%v", tools, err)
	}
	for i, n := range names {
		if tools[i].Name != n || tools[i].Type != types.ToolTypeDynamic {
			t.Errorf("tool %d = %+v", i, tools[i])
		}
	}
}
