package fx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/fx/internal/libfx"
	"github.com/digitallysavvy/go-ai/pkg/mcp"
)

// Port of TS fx-mcp.test.ts, plus libfx createMcpAdapter behavior.

type fakeMCP struct {
	mu         sync.Mutex
	instr      string
	pages      [][]mcp.MCPTool
	cursors    []string
	listErr    error
	listCalls  []string
	callResult *mcp.CallToolResult
	callErr    error
	called     []string
	calledArgs []map[string]any
	closes     int
}

func (f *fakeMCP) listTools(_ context.Context, cursor string) ([]mcp.MCPTool, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls = append(f.listCalls, cursor)
	if f.listErr != nil {
		return nil, "", f.listErr
	}
	i := len(f.listCalls) - 1
	if i >= len(f.pages) {
		return nil, "", nil
	}
	next := ""
	if i < len(f.cursors) {
		next = f.cursors[i]
	}
	return f.pages[i], next, nil
}

func (f *fakeMCP) callTool(_ context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = append(f.called, name)
	f.calledArgs = append(f.calledArgs, args)
	return f.callResult, f.callErr
}

func (f *fakeMCP) instructions() string { return f.instr }
func (f *fakeMCP) close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	return nil
}

// stubConnect replaces connectMCP for one test, handing out clients in
// order (or an error) and recording the configurations.
func stubConnect(t *testing.T, clients ...any) *[]fxMCPServerConfig {
	t.Helper()
	orig := connectMCP
	t.Cleanup(func() { connectMCP = orig })
	var mu sync.Mutex
	configs := &[]fxMCPServerConfig{}
	connectMCP = func(_ context.Context, cfg fxMCPServerConfig) (mcpSession, error) {
		mu.Lock()
		defer mu.Unlock()
		*configs = append(*configs, cfg)
		if len(clients) == 0 {
			return nil, errors.New("unexpected MCP connection")
		}
		next := clients[0]
		clients = clients[1:]
		if err, ok := next.(error); ok {
			return nil, err
		}
		return next.(mcpSession), nil
	}
	return configs
}

func TestFxMCPConfiguresTransport(t *testing.T) {
	for _, tc := range []struct {
		typ      any
		expected string
	}{{nil, ""}, {"http", "http"}, {"streamableHttp", "streamableHttp"}, {"sse", "sse"}} {
		client := &fakeMCP{instr: "Server instructions"}
		configs := stubConnect(t, client)
		entry := map[string]any{"url": "https://mcp.context7.com/mcp", "headers": map[string]string{"x-api-key": "test-key"}, "disabled": false, "extraField": "accepted"}
		if tc.typ != nil {
			entry["type"] = tc.typ
		}
		rt, err := createFxMCPRuntime(context.Background(), map[string]any{"context7": entry}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(*configs) != 1 || (*configs)[0].URL != "https://mcp.context7.com/mcp" || (*configs)[0].Type != tc.expected || (*configs)[0].Headers["x-api-key"] != "test-key" {
			t.Fatalf("%v: configs %+v", tc.typ, *configs)
		}
		if rt.instructions != "Server instructions" || len(rt.tools) != 0 || len(client.listCalls) != 1 || client.listCalls[0] != "" {
			t.Fatalf("%v: runtime %+v, list calls %v", tc.typ, rt, client.listCalls)
		}
		if err := rt.close(); err != nil || client.closes != 1 {
			t.Fatalf("close %v, closes %d", err, client.closes)
		}
	}
}

func TestFxMCPSkipsDisabledEntries(t *testing.T) {
	configs := stubConnect(t)
	rt, err := createFxMCPRuntime(context.Background(), map[string]any{"disabled": map[string]any{"disabled": true, "headers": []any{}, "type": "stdio"}}, nil)
	if err != nil || len(*configs) != 0 || len(rt.tools) != 0 {
		t.Fatalf("%v %v %v", err, *configs, rt)
	}
}

func TestFxMCPRejectsUnsupportedServers(t *testing.T) {
	for _, config := range []any{
		nil, "invalid", []any{}, map[string]any{}, map[string]any{"command": "server"}, map[string]any{"url": 123},
		map[string]any{"url": "https://mcp.context7.com/mcp", "type": "stdio"},
		map[string]any{"url": "https://mcp.context7.com/mcp", "type": "websocket"},
	} {
		configs := stubConnect(t)
		_, err := createFxMCPRuntime(context.Background(), map[string]any{"context7": config}, nil)
		if !harness.IsCapabilityUnsupportedError(err) {
			t.Fatalf("%v: err = %v", config, err)
		}
		if !strings.Contains(err.Error(), "fx MCP server context7 must use an HTTP or SSE URL. Command/stdio servers are not supported.") {
			t.Fatalf("%v: err = %v", config, err)
		}
		if len(*configs) != 0 {
			t.Fatal("connected")
		}
	}
}

func TestFxMCPIdentifiesInvalidFields(t *testing.T) {
	for _, tc := range []struct {
		fields map[string]any
		field  string
	}{
		{map[string]any{"headers": []any{}}, "headers"},
		{map[string]any{"headers": map[string]any{"authorization": 123}}, "headers.authorization"},
		{map[string]any{"disabled": "false"}, "disabled"},
	} {
		configs := stubConnect(t)
		entry := map[string]any{"url": "https://mcp.context7.com/mcp"}
		for k, v := range tc.fields {
			entry[k] = v
		}
		_, err := createFxMCPRuntime(context.Background(), map[string]any{"context7": entry}, nil)
		if err == nil || !strings.HasPrefix(err.Error(), "Invalid fx MCP server context7 field "+tc.field+":") {
			t.Fatalf("err = %v", err)
		}
		if len(*configs) != 0 {
			t.Fatal("connected")
		}
	}
}

func TestFxMCPClosesClientsAfterLaterFailure(t *testing.T) {
	for _, failure := range []string{"configuration", "initialization", "catalog"} {
		t.Run(failure, func(t *testing.T) {
			first, second := &fakeMCP{}, &fakeMCP{}
			clients := []any{first}
			switch failure {
			case "initialization":
				clients = append(clients, errors.New("initialization failed"))
			case "catalog":
				second.listErr = errors.New("catalog failed")
				clients = append(clients, second)
			}
			stubConnect(t, clients...)
			secondConfig := any(map[string]any{"url": "https://mcp.context7.com/mcp"})
			if failure == "configuration" {
				secondConfig = map[string]any{"headers": []any{}}
			}
			_, err := createFxMCPRuntime(context.Background(), map[string]any{
				"first":  map[string]any{"url": "https://mcp.context7.com/mcp"},
				"second": secondConfig,
			}, nil)
			if err == nil {
				t.Fatal("no error")
			}
			want := 0
			if failure == "catalog" {
				want = 1
			}
			if first.closes != 1 || second.closes != want {
				t.Fatalf("closes first %d second %d", first.closes, second.closes)
			}
		})
	}
}

func TestMCPAdapterNamesAndPagination(t *testing.T) {
	long := strings.Repeat("x", 70)
	client := &fakeMCP{
		pages: [][]mcp.MCPTool{
			{{Name: "search docs"}, {Name: "search_docs", Description: "second"}},
			{{Name: long}, {Name: long + "y"}, {Name: "émoji😀"}},
		},
		cursors: []string{"page-2"},
	}
	tools, err := createMCPAdapter(context.Background(), client, "mcp_my_server_")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools {
		names = append(names, tool.name)
	}
	longBase := ("mcp_my_server_" + long)[:64]
	want := []string{"mcp_my_server_search_docs", "mcp_my_server_search_docs_2", longBase, longBase[:62] + "_2", "mcp_my_server__moji__"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("names\n%v\nwant\n%v", names, want)
	}
	if tools[0].description != "MCP tool" || tools[1].description != "second" {
		t.Fatalf("descriptions %q %q", tools[0].description, tools[1].description)
	}
	if mustJSON(tools[0].inputSchema) != `{"properties":{},"type":"object"}` {
		t.Fatalf("schema %s", mustJSON(tools[0].inputSchema))
	}
	if strings.Join(client.listCalls, ",") != ",page-2" {
		t.Fatalf("list calls %q", client.listCalls)
	}
	if _, err := tools[1].execute(context.Background(), json.RawMessage(`{"q":"go"}`)); err != nil {
		t.Fatal(err)
	}
	if client.called[0] != "search_docs" || client.calledArgs[0]["q"] != "go" {
		t.Fatalf("called %v %v", client.called, client.calledArgs)
	}
}

func TestMCPAdapterRejectsInvalidCatalogs(t *testing.T) {
	many := make([]mcp.MCPTool, 65)
	for i := range many {
		many[i] = mcp.MCPTool{Name: fmt.Sprintf("t%d", i)}
	}
	for _, tc := range []struct {
		name   string
		client *fakeMCP
		want   string
	}{
		{"too many", &fakeMCP{pages: [][]mcp.MCPTool{many}}, "MCP listTools() returned an invalid tool catalog"},
		{"repeated cursor", &fakeMCP{pages: [][]mcp.MCPTool{{{Name: "a"}}, {{Name: "b"}}}, cursors: []string{"c", "c"}}, "MCP listTools() returned an invalid pagination cursor"},
		{"duplicate", &fakeMCP{pages: [][]mcp.MCPTool{{{Name: "a"}, {Name: "a"}}}}, "MCP tool a is duplicated"},
		{"unnamed", &fakeMCP{pages: [][]mcp.MCPTool{{{Name: ""}}}}, "MCP tool 0 is invalid"},
		{"long cursor", &fakeMCP{pages: [][]mcp.MCPTool{{{Name: "a"}}}, cursors: []string{strings.Repeat("c", 4097)}}, "MCP listTools() returned an invalid pagination cursor"},
	} {
		if _, err := createMCPAdapter(context.Background(), tc.client, "p_"); err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	endless := &fakeMCP{}
	for i := 0; i < 70; i++ {
		endless.pages = append(endless.pages, nil)
		endless.cursors = append(endless.cursors, fmt.Sprintf("c%d", i))
	}
	if _, err := createMCPAdapter(context.Background(), endless, "p_"); err == nil || err.Error() != "MCP tool pagination exceeded its page limit" {
		t.Errorf("endless: %v", err)
	}
}

func TestMCPToolOutput(t *testing.T) {
	text := func(s string) mcp.ToolResultContent { return mcp.ToolResultContent{Type: "text", Text: s} }
	out, err := mcpToolOutput("t", &mcp.CallToolResult{Content: []mcp.ToolResultContent{
		text("hello"),
		{Type: "resource", Resource: &mcp.ResourceContent{URI: "file://a", Text: "resource text"}},
		{Type: "resource_link", URI: "file://b", Name: "B"},
		{Type: "resource_link", URI: "file://c"},
	}})
	if err != nil || out != "hello\nresource text\nB: file://b\nResource: file://c" {
		t.Fatalf("%q %v", out, err)
	}
	out, _ = mcpToolOutput("t", &mcp.CallToolResult{Content: []mcp.ToolResultContent{text("x")}, StructuredContent: map[string]any{"a": 1}})
	if out != "x\n{\"a\":1}" {
		t.Fatalf("structured %q", out)
	}
	out, _ = mcpToolOutput("t", &mcp.CallToolResult{Content: []mcp.ToolResultContent{text(`{"a":1}`)}, StructuredContent: map[string]any{"a": 1}})
	if out != `{"a":1}` {
		t.Fatalf("same structured %q", out)
	}
	out, _ = mcpToolOutput("t", &mcp.CallToolResult{Content: []mcp.ToolResultContent{
		text("chart"), {Type: "image", MimeType: "image/png", Data: "AAA"},
		{Type: "resource", Resource: &mcp.ResourceContent{URI: "r", MimeType: "image/jpeg", Blob: "BBB"}},
	}})
	rich, ok := out.(*libfx.TypedToolResult)
	if !ok || rich.Text != "chart" || len(rich.Images) != 2 || rich.Images[1].MimeType != "image/jpeg" || rich.Images[1].Data != "BBB" {
		t.Fatalf("rich %+v", out)
	}
	if _, err := mcpToolOutput("t", &mcp.CallToolResult{IsError: true}); err == nil || err.Error() != "MCP tool t failed" {
		t.Fatalf("error %v", err)
	}
	if _, err := mcpToolOutput("t", &mcp.CallToolResult{IsError: true, Content: []mcp.ToolResultContent{text("boom")}}); err == nil || err.Error() != "boom" {
		t.Fatalf("error %v", err)
	}
	_, err = mcpToolOutput("t", &mcp.CallToolResult{IsError: true, Content: []mcp.ToolResultContent{text("bad"), {Type: "image", MimeType: "image/png", Data: "AAA"}}})
	var richErr *libfx.ToolResultError
	if !errors.As(err, &richErr) || richErr.Result.IsError == nil || !*richErr.Result.IsError || len(richErr.Result.Images) != 1 {
		t.Fatalf("rich error %v", err)
	}
}

// TestLibfxMCPToolsReachTheRuntime covers the session side of TS
// fx-session.ts: MCP tools follow built-in and custom tools, a name that
// collides is renamed name_2, the tools run as provider-executed bash-kind
// tools, server instructions join the runtime instructions, and clients
// close when the turn ends.
func TestLibfxMCPToolsReachTheRuntime(t *testing.T) {
	client := &fakeMCP{instr: "Use the docs server.", pages: [][]mcp.MCPTool{{{Name: "lookup", Description: "Look up"}}}, callResult: &mcp.CallToolResult{Content: []mcp.ToolResultContent{{Type: "text", Text: "found"}}}}
	stubConnect(t, client)
	h, m := newMockHarness(t, Settings{MCPServers: map[string]any{"docs": map[string]any{"url": "https://mcp.example/mcp"}}})
	sb := newFakeSandbox()
	s, err := h.DoStart(context.Background(), startOpts(sb, "mcp"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DoDestroy(context.Background()) //nolint:errcheck
	if err := doTurn(t, s, harness.PromptTurnOptions{TurnSettings: harness.TurnSettings{Tools: []harness.ToolSpec{{Name: "mcp_docs_lookup"}}}, Prompt: harness.TextPrompt("go")}); err != nil {
		t.Fatal(err)
	}
	o := m.opts()[0]
	var names []string
	for _, tool := range o.Tools {
		names = append(names, tool.Name)
	}
	joined := strings.Join(names, ",")
	if !strings.HasSuffix(joined, ",mcp_docs_lookup,mcp_docs_lookup_2") {
		t.Fatalf("tools = %s", joined)
	}
	if !strings.Contains(o.Instructions, "Use the docs server.") {
		t.Fatalf("instructions = %q", o.Instructions)
	}
	if client.closes != 1 {
		t.Fatalf("closes = %d", client.closes)
	}

	// Without the collision, the MCP tool runs and its call is dynamic.
	client2 := &fakeMCP{pages: [][]mcp.MCPTool{{{Name: "lookup"}}}, callResult: &mcp.CallToolResult{Content: []mcp.ToolResultContent{{Type: "text", Text: "found"}}}}
	stubConnect(t, client2)
	m.setRun(func(ctx context.Context, o runtimeOptions) error {
		out, err := invoke(o, "mcp_docs_lookup", "call_1", map[string]any{"q": "x"})
		if err != nil {
			return err
		}
		if r, ok := out.(*libfx.TypedToolResult); !ok || r.Text != "found" {
			return fmt.Errorf("tool output %+v", out)
		}
		return nil
	})
	s2, err := h.DoStart(context.Background(), startOpts(newFakeSandbox(), "mcp-2"))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.DoDestroy(context.Background()) //nolint:errcheck
	log2 := &partLog{}
	if err := doTurn(t, s2, harness.PromptTurnOptions{Prompt: harness.TextPrompt("go"), Emit: log2.emit}); err != nil {
		t.Fatal(err)
	}
	if len(client2.called) != 1 || client2.called[0] != "lookup" || client2.calledArgs[0]["q"] != "x" {
		t.Fatalf("called %v", client2.called)
	}
	calls := log2.ofType(harness.PartTypeToolCall)
	if len(calls) != 1 || !calls[0].(*harness.ToolCallPart).Dynamic || !calls[0].(*harness.ToolCallPart).ProviderExecuted {
		t.Fatalf("tool calls %+v", calls)
	}
}

func TestLibfxMCPStdioServerFailsTheTurn(t *testing.T) {
	h, m := newMockHarness(t, Settings{MCPServers: map[string]any{"local": map[string]any{"command": "server"}}})
	s, err := h.DoStart(context.Background(), startOpts(newFakeSandbox(), "mcp-stdio"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DoDestroy(context.Background()) //nolint:errcheck
	if err := doTurn(t, s, harness.PromptTurnOptions{Prompt: harness.TextPrompt("go")}); !harness.IsCapabilityUnsupportedError(err) {
		t.Fatalf("err = %v", err)
	}
	if len(m.opts()) != 0 {
		t.Fatal("the runtime started")
	}
}

// TS connects mcpServers in object key order; Go takes MCPServerOrder,
// then the names it omits sorted, and ignores names it does not configure.
func TestFxMCPServerOrder(t *testing.T) {
	servers := func() map[string]any {
		return map[string]any{
			"zeta":  map[string]any{"url": "https://zeta.example/mcp"},
			"alpha": map[string]any{"url": "https://alpha.example/mcp"},
			"mid":   map[string]any{"url": "https://mid.example/mcp"},
		}
	}
	for _, tc := range []struct {
		order []string
		want  []string
	}{
		{nil, []string{"alpha", "mid", "zeta"}},
		{[]string{"zeta", "mid", "alpha"}, []string{"zeta", "mid", "alpha"}},
		{[]string{"missing", "zeta", "zeta"}, []string{"zeta", "alpha", "mid"}},
	} {
		var clients []any
		for range 3 {
			clients = append(clients, &fakeMCP{pages: [][]mcp.MCPTool{{{Name: "t"}}}})
		}
		configs := stubConnect(t, clients...)
		rt, err := createFxMCPRuntime(context.Background(), servers(), tc.order)
		if err != nil {
			t.Fatal(err)
		}
		var urls, tools []string
		for _, c := range *configs {
			urls = append(urls, c.URL)
		}
		for _, tool := range rt.tools {
			tools = append(tools, tool.name)
		}
		var wantURLs, wantTools []string
		for _, name := range tc.want {
			wantURLs = append(wantURLs, "https://"+name+".example/mcp")
			wantTools = append(wantTools, "mcp_"+name+"_t")
		}
		if strings.Join(urls, ",") != strings.Join(wantURLs, ",") || strings.Join(tools, ",") != strings.Join(wantTools, ",") {
			t.Errorf("order %v: connected %v, tools %v", tc.order, urls, tools)
		}
		_ = rt.close()
	}
}
