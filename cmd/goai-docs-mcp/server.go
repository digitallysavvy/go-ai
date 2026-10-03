package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/cmd/goai-docs-mcp/internal/docs"
)

// JSON-RPC and MCP constants.
const (
	latestProtocolVersion = "2025-06-18"

	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

var supportedProtocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Server is a minimal MCP server (tools only) over a newline-delimited
// JSON-RPC stdio transport.
type Server struct {
	index   *docs.Index
	version string

	mu  sync.Mutex // guards out
	out io.Writer
}

// NewServer returns a server that answers from index.
func NewServer(index *docs.Index, version string) *Server {
	return &Server{index: index, version: version}
}

// Serve reads one JSON-RPC message per line from r and writes responses to w
// until r is closed. Messages are processed in order.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	s.out = w
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			s.handleLine(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func (s *Server) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// One message per line; json.Marshal never emits raw newlines.
	_, _ = s.out.Write(append(b, '\n'))
}

func (s *Server) replyError(id json.RawMessage, code int, msg string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	s.send(response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func (s *Server) handleLine(line []byte) {
	trimmed := strings.TrimSpace(string(line))
	if strings.HasPrefix(trimmed, "[") {
		s.replyError(nil, codeInvalidRequest, "batch requests are not supported")
		return
	}
	var req request
	if err := json.Unmarshal([]byte(trimmed), &req); err != nil {
		s.replyError(nil, codeParseError, "parse error: "+err.Error())
		return
	}
	isNotification := len(req.ID) == 0
	if req.JSONRPC != "2.0" || req.Method == "" {
		// A response from the client, or malformed input. Responses need no reply.
		if req.Method == "" && !isNotification {
			return
		}
		if !isNotification {
			s.replyError(req.ID, codeInvalidRequest, "invalid request")
		}
		return
	}
	if isNotification {
		return // notifications/initialized, notifications/cancelled, ...
	}

	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "ping":
		s.send(response{JSONRPC: "2.0", ID: req.ID, Result: struct{}{}})
	case "tools/list":
		s.send(response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": toolDefinitions()}})
	case "tools/call":
		s.handleToolsCall(req)
	default:
		s.replyError(req.ID, codeMethodNotFound, "method not found: "+req.Method)
	}
}

func (s *Server) handleInitialize(req request) {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			s.replyError(req.ID, codeInvalidParams, "invalid initialize params")
			return
		}
	}
	version := latestProtocolVersion
	for _, v := range supportedProtocolVersions {
		if v == p.ProtocolVersion {
			version = v
		}
	}
	s.send(response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      map[string]any{"name": "goai-docs", "title": "Go AI SDK docs", "version": s.version},
		"instructions": "Search and read the Go AI SDK documentation. Call search_docs first, " +
			"then read_doc with a result's path. Call list_docs to browse a section.",
	}})
}

func toolDefinitions() []map[string]any {
	readOnly := map[string]any{"readOnlyHint": true, "openWorldHint": false}
	return []map[string]any{
		{
			"name":        "search_docs",
			"title":       "Search Go AI SDK docs",
			"description": "Search the Go AI SDK documentation. Returns the best matching pages with a path, type and snippet. Pass a result's path to read_doc.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search terms, for example \"tool approval\" or \"StreamText chunks\"."},
					"type":  map[string]any{"type": "string", "enum": []string{"guide", "reference", "provider", "migration", "troubleshooting", "recipe"}, "description": "Only return pages of this type."},
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 25, "description": "Maximum results (default 8)."},
				},
				"required": []string{"query"},
			},
			"annotations": readOnly,
		},
		{
			"name":        "read_doc",
			"title":       "Read a Go AI SDK docs page",
			"description": "Return one documentation page as markdown. The path comes from search_docs or list_docs, for example /docs/agents/building-agents.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Page path such as /docs/getting-started/golang. A goaisdk.com URL or a trailing .md also works."},
				},
				"required": []string{"path"},
			},
			"annotations": readOnly,
		},
		{
			"name":        "list_docs",
			"title":       "List Go AI SDK docs pages",
			"description": "List documentation pages with their path, type and description, optionally for one section such as agents or ai-sdk-core.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"section": map[string]any{"type": "string", "description": "Section slug, for example agents. Omit to list the sections."},
				},
			},
			"annotations": readOnly,
		},
	}
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func toolResult(text string, isError bool) map[string]any {
	res := map[string]any{"content": []toolContent{{Type: "text", Text: text}}}
	if isError {
		res["isError"] = true
	}
	return res
}

func (s *Server) handleToolsCall(req request) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
		s.replyError(req.ID, codeInvalidParams, "tools/call needs a tool name")
		return
	}
	args := p.Arguments
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	var result map[string]any
	switch p.Name {
	case "search_docs":
		var a struct {
			Query string `json:"query"`
			Type  string `json:"type"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			s.replyError(req.ID, codeInvalidParams, "invalid arguments: "+err.Error())
			return
		}
		result = s.searchDocs(a.Query, a.Type, a.Limit)
	case "read_doc":
		var a struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			s.replyError(req.ID, codeInvalidParams, "invalid arguments: "+err.Error())
			return
		}
		result = s.readDoc(a.Path)
	case "list_docs":
		var a struct {
			Section string `json:"section"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			s.replyError(req.ID, codeInvalidParams, "invalid arguments: "+err.Error())
			return
		}
		result = s.listDocs(a.Section)
	default:
		s.replyError(req.ID, codeInvalidParams, "unknown tool: "+p.Name)
		return
	}
	s.send(response{JSONRPC: "2.0", ID: req.ID, Result: result})
}

func (s *Server) searchDocs(query, typ string, limit int) map[string]any {
	if strings.TrimSpace(query) == "" {
		return toolResult("query is required", true)
	}
	if limit > 25 {
		limit = 25
	}
	hits := s.index.Search(query, typ, limit)
	if len(hits) == 0 {
		return toolResult(fmt.Sprintf("No pages matched %q. Try fewer or different words, or call list_docs.", query), false)
	}
	var b strings.Builder
	for i, h := range hits {
		fmt.Fprintf(&b, "%d. %s\n   path: %s\n   type: %s\n   url: %s\n", i+1, h.Doc.Title, h.Doc.Path, h.Doc.Type, h.Doc.URL())
		if h.Snippet != "" {
			fmt.Fprintf(&b, "   %s\n", h.Snippet)
		}
	}
	return toolResult(strings.TrimRight(b.String(), "\n"), false)
}

func (s *Server) readDoc(path string) map[string]any {
	if strings.TrimSpace(path) == "" {
		return toolResult("path is required", true)
	}
	d, ok := s.index.Lookup(path)
	if !ok {
		return toolResult(fmt.Sprintf("No page at %q. Use search_docs or list_docs to find a path.", path), true)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", d.Title)
	if d.Description != "" {
		fmt.Fprintf(&b, "> %s\n\n", d.Description)
	}
	fmt.Fprintf(&b, "Canonical URL: %s%s\n\n", docs.SiteURL, d.Path)
	b.WriteString(d.Body)
	return toolResult(b.String(), false)
}

func (s *Server) listDocs(section string) map[string]any {
	all := s.index.Docs()
	var b strings.Builder
	section = strings.Trim(strings.ToLower(section), "/ ")
	if section == "" {
		counts := map[string]int{}
		for _, d := range all {
			counts[d.Section]++
		}
		names := make([]string, 0, len(counts))
		for n := range counts {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			label := n
			if label == "" {
				label = "(root pages)"
			}
			fmt.Fprintf(&b, "- %s: %d pages\n", label, counts[n])
		}
		return toolResult("Sections (pass one as section):\n"+b.String(), false)
	}
	for _, d := range all {
		if d.Section != section {
			continue
		}
		fmt.Fprintf(&b, "- %s (%s) %s", d.Title, d.Type, d.Path)
		if d.Description != "" {
			fmt.Fprintf(&b, ": %s", d.Description)
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return toolResult(fmt.Sprintf("No section %q. Call list_docs with no arguments to see the sections.", section), true)
	}
	return toolResult(b.String(), false)
}
