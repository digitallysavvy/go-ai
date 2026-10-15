package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// Drives the TS vitest snapshots of anthropic-language-model.test.ts
// (testdata/ts/snapshots.json, extracted from __snapshots__) through the Go
// provider: the sequence of parts (type and tool name), finish reasons and
// token totals must equal what TS recorded.

type tsPart struct {
	Type         string `json:"type"`
	ToolName     string `json:"toolName"`
	ToolCallID   string `json:"toolCallId"`
	Kind         string `json:"kind"`
	FinishReason *struct {
		Raw     string `json:"raw"`
		Unified string `json:"unified"`
	} `json:"finishReason"`
	Usage *struct {
		InputTokens  *int `json:"inputTokens"`
		OutputTokens *int `json:"outputTokens"`
	} `json:"usage"`
}

type tsSnapshot struct {
	Kind  string   `json:"kind"`
	Parts []tsPart `json:"parts"`
}

func loadTSSnapshots(t *testing.T) map[string]tsSnapshot {
	t.Helper()
	var m map[string]tsSnapshot
	if err := json.Unmarshal(tsFixture(t, "snapshots.json"), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func tsSeq(parts []tsPart, stream bool) []string {
	var out []string
	for _, p := range parts {
		name := p.Type
		switch name {
		case "text-delta":
			name = "text"
		case "reasoning-delta":
			name = "reasoning"
		}
		switch p.Type {
		case "tool-call", "tool-result", "tool-input-start":
			name += ":" + p.ToolName
		}
		if n := len(out); n > 0 && out[n-1] == name && stream && (name == "text" || name == "reasoning" || name == "tool-input-delta") {
			continue
		}
		out = append(out, name)
	}
	return out
}

func goStreamSeq(chunks []*provider.StreamChunk) []string {
	var out []string
	for i, c := range chunks {
		// providerutils.WithResponseMetadata prepends one response-metadata
		// chunk that carries the HTTP headers (TS returns them on the result,
		// not in the stream); the message_start one follows it.
		if c.Type == provider.ChunkTypeResponseMetadata && i > 0 && chunks[i-1].Type == provider.ChunkTypeResponseMetadata {
			continue
		}
		name := string(c.Type)
		switch c.Type {
		case provider.ChunkTypeToolCall, provider.ChunkTypeToolInputStart:
			if c.ToolCall != nil {
				name += ":" + tsToolName(c.ToolCall.ToolName)
			}
		case provider.ChunkTypeToolResult:
			if c.ToolResult != nil {
				name += ":" + tsToolName(c.ToolResult.ToolName)
			}
		case provider.ChunkTypeToolInputEnd:
		}
		if n := len(out); n > 0 && out[n-1] == name && (name == "text" || name == "reasoning" || name == "tool-input-delta") {
			continue
		}
		out = append(out, name)
	}
	return out
}

func goContentSeq(content []types.ContentPart) []string {
	var out []string
	for _, c := range content {
		switch p := c.(type) {
		case types.ToolCallContent:
			out = append(out, "tool-call:"+tsToolName(p.ToolName))
		case types.ToolResultContent:
			out = append(out, "tool-result:"+tsToolName(p.ToolName))
		case types.SourceContent:
			out = append(out, "source")
		case types.TextContent:
			out = append(out, "text")
		case types.ReasoningContent:
			out = append(out, "reasoning")
		case types.GeneratedFileContent:
			out = append(out, "file")
		default:
			out = append(out, fmt.Sprintf("%T", c))
		}
	}
	return out
}

type snapCase struct {
	name    string // snapshot key suffix after "AnthropicLanguageModel > "
	fixture string
	tools   []types.Tool
	format  *provider.ResponseFormat
	// tsEmptyEventError: the fixture ends with a newline, so TS also parsed an
	// empty SSE event and recorded a trailing JSON-parse error part. Go's SSE
	// decoder drops data-less events (the real API never sends one).
	tsEmptyEventError bool
}

// tsToolName maps the Go tool name (the provider tool id for provider tools,
// the wire name for unregistered server tools) to the name the TS tests give
// the same tool.
func tsToolName(n string) string {
	n = strings.TrimPrefix(n, "anthropic.")
	for _, base := range []string{"code_execution", "web_search", "web_fetch", "memory"} {
		if strings.HasPrefix(n, base) {
			return base
		}
	}
	if strings.HasPrefix(n, "tool_search") {
		return "tool_search"
	}
	return n
}

func snapCases() []snapCase {
	ce25 := []types.Tool{tools.CodeExecution20250825()}
	ce26 := []types.Tool{tools.CodeExecution20260120()}
	return []snapCase{
		// doGenerate
		{name: "doGenerate > code execution 20250825 > should include code execution tool call and result in content 1", fixture: "anthropic-code-execution-20250825.1", tools: ce25},
		{name: "doGenerate > code execution 20250825 > should include file id list in code execution tool generate call result. 1", fixture: "anthropic-code-execution-20250825.2", tools: ce25},
		{name: "doGenerate > code execution 20260120 > should include code execution tool call and result in content 1", fixture: "anthropic-code-execution-20250825.1", tools: ce26},
		{name: "doGenerate > mcp servers > should include mcp tool call and result in content 1", fixture: "anthropic-mcp.1"},
		{name: "doGenerate > memory 20250818 > should include memory tool call and result in content 1", fixture: "anthropic-memory-20250818.1", tools: []types.Tool{tools.Memory20250818()}},
		{name: "doGenerate > programmatic tool calling > with fixture (multi-turn dice game) > should parse content with text, server_tool_use, tool_use with caller, and code_execution_tool_result 1", fixture: "anthropic-programmatic-tool-calling.1", tools: ce25},
		{name: "doGenerate > tool search tool > bm25 variant > should include tool search tool call and result in content 1", fixture: "anthropic-tool-search-bm25.1", tools: []types.Tool{tools.ToolSearchBm2520251119()}},
		{name: "doGenerate > tool search tool > regex variant > should include tool search tool call and result in content 1", fixture: "anthropic-tool-search-regex.1", tools: []types.Tool{tools.ToolSearchRegex20251119()}},
		{name: "doGenerate > web fetch tool > 20260209 text response > should include web fetch 20260209 tool call and result in content 1", fixture: "anthropic-web-fetch-tool-20260209.1", tools: []types.Tool{tools.WebFetch20260209(tools.WebFetch20260209Config{}), tools.CodeExecution20260120()}},
		{name: "doGenerate > web fetch tool > text response > should include web fetch tool call and result in content 1", fixture: "anthropic-web-fetch-tool.1", tools: []types.Tool{tools.WebFetch20250910(tools.WebFetch20260209Config{})}},
		{name: "doGenerate > web fetch tool > text response without title > should include web fetch tool call and result in content 1", fixture: "anthropic-web-fetch-tool.2", tools: []types.Tool{tools.WebFetch20250910(tools.WebFetch20260209Config{})}},
		{name: "doGenerate > web fetch tool > unavailable error > should include web fetch tool call and result in content 1", fixture: "anthropic-web-fetch-tool.error", tools: []types.Tool{tools.WebFetch20250910(tools.WebFetch20260209Config{})}},
		{name: "doGenerate > web search tool > with fixture > should include web search tool call and result in content 1", fixture: "anthropic-web-search-tool.1", tools: []types.Tool{tools.WebSearch20250305(tools.WebSearch20260209Config{})}},
		// doStream
		{name: "doStream > agent skills > should stream code execution tool results 1", fixture: "anthropic-code-execution-20250825.pptx-skill", tools: ce25},
		{name: "doStream > code execution 20250825 tool > should include file id list in code execution tool call result. 1", fixture: "anthropic-code-execution-20250825.2", tools: ce25, tsEmptyEventError: true},
		{name: "doStream > code execution 20250825 tool > should stream code execution tool results 1", fixture: "anthropic-code-execution-20250825.1", tools: ce25},
		{name: "doStream > code execution 20260120 tool > should stream code execution tool results without beta header 1", fixture: "anthropic-code-execution-20250825.1", tools: ce26},
		{name: "doStream > json schema response format with output format (supported model) > should stream the text output 1", fixture: "anthropic-json-output-format.1", format: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}}, "required": []interface{}{"name"}, "additionalProperties": false}}},
		{name: "doStream > mcp servers > should stream code execution tool results 1", fixture: "anthropic-mcp.1"},
		{name: "doStream > programmatic tool calling > with fixture (multi-turn dice game) > should stream programmatic tool calling with multiple message_start/stop sequences 1", fixture: "anthropic-programmatic-tool-calling.1", tools: ce25},
		{name: "doStream > tool search tool > bm25 variant > should stream tool search bm25 results 1", fixture: "anthropic-tool-search-bm25.1", tools: []types.Tool{tools.ToolSearchBm2520251119()}},
		{name: "doStream > tool search tool > regex variant > should stream tool search regex results 1", fixture: "anthropic-tool-search-regex.1", tools: []types.Tool{tools.ToolSearchRegex20251119()}},
		{name: "doStream > web fetch 20260209 tool > input provided in content_block_start > should stream web fetch 20260209 tool results 1", fixture: "anthropic-web-fetch-tool-20260209.1", tools: []types.Tool{tools.WebFetch20260209(tools.WebFetch20260209Config{}), tools.CodeExecution20260120()}},
		{name: "doStream > web fetch tool > txt response > should stream web search tool results 1", fixture: "anthropic-web-fetch-tool.1", tools: []types.Tool{tools.WebFetch20250910(tools.WebFetch20260209Config{})}},
		{name: "doStream > web search tool > should stream web search tool results 1", fixture: "anthropic-web-search-tool.1", tools: []types.Tool{tools.WebSearch20250305(tools.WebSearch20260209Config{})}},
	}
}

func TestTSSnapshotsDrivenThroughGo(t *testing.T) {
	snaps := loadTSSnapshots(t)
	for _, sc := range snapCases() {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			snap, ok := snaps["AnthropicLanguageModel > "+strings.TrimSuffix(sc.name, " 1")]
			if !ok {
				t.Fatalf("no snapshot %q", sc.name)
			}
			opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: sc.tools, ResponseFormat: sc.format}
			if snap.Kind == "generate" {
				srv, closeFn := fixtureModel(t, serveJSONFixture(sc.fixture, t))
				defer closeFn()
				res, err := srv.DoGenerate(context.Background(), opts)
				if err != nil {
					t.Fatal(err)
				}
				got, want := goContentSeq(res.Content), tsSeq(snap.Parts, false)
				if strings.Join(got, ",") != strings.Join(want, ",") {
					t.Fatalf("content\n got %v\nwant %v", got, want)
				}
				return
			}
			m, closeFn := fixtureModel(t, serveChunksFixture(sc.fixture, t))
			defer closeFn()
			s, err := m.DoStream(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			chunks := drain(t, s)
			got, want := goStreamSeq(chunks), tsSeq(snap.Parts, true)
			if sc.tsEmptyEventError && len(want) > 0 && want[len(want)-1] == "error" {
				want = want[:len(want)-1]
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("stream\n got %v\nwant %v", got, want)
			}
			var gotFin []*provider.StreamChunk
			for _, c := range chunks {
				if c.Type == provider.ChunkTypeFinish {
					gotFin = append(gotFin, c)
				}
			}
			var wantFin []tsPart
			for _, p := range snap.Parts {
				if p.Type == "finish" {
					wantFin = append(wantFin, p)
				}
			}
			if len(gotFin) != len(wantFin) {
				t.Fatalf("finishes %d vs %d", len(gotFin), len(wantFin))
			}
			for i, w := range wantFin {
				g := gotFin[i]
				if string(g.FinishReason) != w.FinishReason.Unified || g.RawFinishReason != w.FinishReason.Raw {
					t.Errorf("finish %d = %v/%q want %s/%s", i, g.FinishReason, g.RawFinishReason, w.FinishReason.Unified, w.FinishReason.Raw)
				}
				if w.Usage != nil && w.Usage.InputTokens != nil && (g.Usage == nil || g.Usage.InputTokens == nil || int(*g.Usage.InputTokens) != *w.Usage.InputTokens) {
					t.Errorf("finish %d input tokens = %+v want %d", i, g.Usage, *w.Usage.InputTokens)
				}
				if w.Usage != nil && w.Usage.OutputTokens != nil && (g.Usage == nil || g.Usage.OutputTokens == nil || int(*g.Usage.OutputTokens) != *w.Usage.OutputTokens) {
					t.Errorf("finish %d output tokens = %+v want %d", i, g.Usage, *w.Usage.OutputTokens)
				}
			}
		})
	}
}
