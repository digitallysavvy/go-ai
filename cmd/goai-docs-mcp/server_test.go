package main

import (
	"bufio"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	godocs "github.com/digitallysavvy/go-ai/docs"

	"github.com/digitallysavvy/go-ai/cmd/goai-docs-mcp/internal/docs"
)

// TestMain lets the test binary act as the server, so the stdio tests drive a
// real child process.
func TestMain(m *testing.M) {
	if os.Getenv("GOAI_DOCS_MCP_CHILD") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

type rpcClient struct {
	t   *testing.T
	in  io.WriteCloser
	out *bufio.Reader
	cmd *exec.Cmd
	id  int
}

func startChild(t *testing.T) *rpcClient {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), "GOAI_DOCS_MCP_CHILD=1")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &rpcClient{t: t, in: in, out: bufio.NewReader(out), cmd: cmd}
	t.Cleanup(func() { _ = in.Close(); _ = cmd.Wait() })
	return c
}

func (c *rpcClient) write(line string) {
	c.t.Helper()
	if _, err := io.WriteString(c.in, line+"\n"); err != nil {
		c.t.Fatal(err)
	}
}

func (c *rpcClient) read() map[string]any {
	c.t.Helper()
	line, err := c.out.ReadString('\n')
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		c.t.Fatalf("response is not JSON: %q", line)
	}
	if m["jsonrpc"] != "2.0" {
		c.t.Fatalf("missing jsonrpc 2.0: %q", line)
	}
	return m
}

func (c *rpcClient) call(method string, params any) map[string]any {
	c.t.Helper()
	c.id++
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method, "params": params})
	c.write(string(b))
	m := c.read()
	if int(m["id"].(float64)) != c.id {
		c.t.Fatalf("id mismatch: %v", m)
	}
	return m
}

func toolText(t *testing.T, m map[string]any) (string, bool) {
	t.Helper()
	res, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", m)
	}
	content := res["content"].([]any)
	isErr, _ := res["isError"].(bool)
	return content[0].(map[string]any)["text"].(string), isErr
}

func TestStdioSession(t *testing.T) {
	c := startChild(t)

	init := c.call("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "0"},
	})
	res := init["result"].(map[string]any)
	if res["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v", res["protocolVersion"])
	}
	if _, ok := res["capabilities"].(map[string]any)["tools"]; !ok {
		t.Errorf("tools capability missing: %v", res["capabilities"])
	}
	if res["serverInfo"].(map[string]any)["name"] != "goai-docs" {
		t.Errorf("serverInfo = %v", res["serverInfo"])
	}

	// A notification gets no response: the next reply must belong to ping.
	c.write(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if m := c.call("ping", nil); m["error"] != nil {
		t.Errorf("ping: %v", m)
	}

	list := c.call("tools/list", nil)
	var names []string
	for _, tool := range list["result"].(map[string]any)["tools"].([]any) {
		tm := tool.(map[string]any)
		names = append(names, tm["name"].(string))
		if tm["inputSchema"].(map[string]any)["type"] != "object" {
			t.Errorf("tool %v: inputSchema is not an object schema", tm["name"])
		}
	}
	if strings.Join(names, ",") != "search_docs,read_doc,list_docs" {
		t.Errorf("tools = %v", names)
	}

	// Search finds the tool-calling guide, then read_doc returns it.
	search := c.call("tools/call", map[string]any{"name": "search_docs", "arguments": map[string]any{"query": "tool calling", "limit": 3}})
	text, isErr := toolText(t, search)
	if isErr || !strings.Contains(text, "path: /docs/") {
		t.Fatalf("search_docs: isError=%v text=%q", isErr, text)
	}
	var path string
	for _, l := range strings.Split(text, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(l), "path: "); ok {
			path = p
			break
		}
	}
	read := c.call("tools/call", map[string]any{"name": "read_doc", "arguments": map[string]any{"path": path}})
	body, isErr := toolText(t, read)
	if isErr || !strings.Contains(body, "Canonical URL: https://goaisdk.com"+path) {
		t.Errorf("read_doc(%s): isError=%v body=%.200q", path, isErr, body)
	}

	// Tool-level failure is a result with isError, not a protocol error.
	missing := c.call("tools/call", map[string]any{"name": "read_doc", "arguments": map[string]any{"path": "/docs/nope/nothing"}})
	if text, isErr := toolText(t, missing); !isErr || !strings.Contains(text, "No page") {
		t.Errorf("missing page: isError=%v text=%q", isErr, text)
	}

	// Protocol errors.
	if m := c.call("tools/call", map[string]any{"name": "nope"}); m["error"].(map[string]any)["code"].(float64) != codeInvalidParams {
		t.Errorf("unknown tool: %v", m)
	}
	if m := c.call("resources/list", nil); m["error"].(map[string]any)["code"].(float64) != codeMethodNotFound {
		t.Errorf("unknown method: %v", m)
	}
	c.write(`{not json`)
	if m := c.read(); m["error"].(map[string]any)["code"].(float64) != codeParseError || m["id"] != nil {
		t.Errorf("parse error response: %v", m)
	}
}

func TestInitializeUnknownVersionGetsLatest(t *testing.T) {
	c := startChild(t)
	m := c.call("initialize", map[string]any{"protocolVersion": "1999-01-01"})
	if got := m["result"].(map[string]any)["protocolVersion"]; got != latestProtocolVersion {
		t.Errorf("protocolVersion = %v, want %s", got, latestProtocolVersion)
	}
}

func TestSearchRanksTitleMatchFirst(t *testing.T) {
	ix := docs.NewIndex([]docs.Doc{
		{Path: "/docs/a/streaming", Title: "Streaming", Body: "Stream text with StreamText. Tool approval is mentioned once."},
		{Path: "/docs/a/tool-approval", Title: "Tool approval", Description: "Require approval before a tool runs.", Body: "Set ToolApproval on a tool."},
		{Path: "/docs/a/other", Title: "Other", Body: "Nothing relevant here."},
	})
	hits := ix.Search("tool approval", "", 5)
	if len(hits) < 2 || hits[0].Doc.Path != "/docs/a/tool-approval" {
		t.Fatalf("hits = %+v", hits)
	}
	if got := ix.Search("ToolApproval", "", 5); len(got) == 0 || got[0].Doc.Path != "/docs/a/tool-approval" {
		t.Errorf("identifier query: %+v", got)
	}
	if d, ok := ix.Lookup("https://goaisdk.com/docs/a/streaming.md"); !ok || d.Title != "Streaming" {
		t.Errorf("Lookup by URL failed")
	}
}

// TestEmbeddedDocsParse fails if any embedded page cannot be parsed, or if a
// docs directory appears at the root that this server has not been checked
// against. When it fails on a new directory, decide whether it is user
// documentation (add it to knownTopDirs) or contributor-only (add it to
// excludedTop in internal/docs).
func TestEmbeddedDocsParse(t *testing.T) {
	pages, err := docs.Build(godocs.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) < 50 {
		t.Errorf("only %d pages embedded", len(pages))
	}
	for _, p := range pages {
		if p.Title == "" || !strings.HasPrefix(p.Path, "/docs") {
			t.Errorf("bad page: %+v", p.Path)
		}
		if strings.Contains(p.Path, "/scripts") || strings.Contains(p.Path, "/templates") {
			t.Errorf("contributor page served: %s", p.Path)
		}
	}
	known := regexp.MustCompile(`^\d\d-[a-z0-9-]+$|^(guides|providers|reference|security|scripts|_templates)$`)
	entries, err := fs.ReadDir(godocs.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() && !known.MatchString(e.Name()) {
			t.Errorf("unexpected top-level docs directory %q; see the test comment", e.Name())
		}
	}
}

func TestStartupTime(t *testing.T) {
	start := time.Now()
	pages, err := docs.Build(godocs.FS)
	if err != nil {
		t.Fatal(err)
	}
	docs.NewIndex(pages)
	t.Logf("built index of %d pages in %v", len(pages), time.Since(start))
	if time.Since(start) > 3*time.Second {
		t.Errorf("startup too slow: %v", time.Since(start))
	}
}
