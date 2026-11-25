package xai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports of the TS tests added by 61ce23efab (x_search) and 6f56c29ff6 (mcp)
// in xai-responses-language-model.test.ts: the tool call keeps the model's
// arguments in input when the call is parsed against the tool's input schema.

var portprovbSearchArguments = map[string]interface{}{"query": "ai sdk", "limit": "5", "mode": "Latest"}

const portprovbSearchArgumentsJSON = `{"query":"ai sdk","limit":"5","mode":"Latest"}`

const portprovbMCPArgumentsJSON = `{"repoName":"vercel/ai"}`

func portprovbParse(t *testing.T, call types.ToolCall, tool types.Tool) types.ToolCall {
	t.Helper()
	parsed, err := ai.ParseToolCall(context.Background(), ai.ParseToolCallOptions{ToolCall: call, Tools: []types.Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Invalid || parsed.Error != nil {
		t.Fatalf("parsed = %#v", parsed)
	}
	return parsed
}

func portprovbModel(t *testing.T, contentType string, body func(w http.ResponseWriter)) *ResponsesLanguageModel {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		body(w)
	}))
	t.Cleanup(server.Close)
	return NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "grok-4-fast-non-reasoning")
}

func portprovbStreamParts(t *testing.T, model *ResponsesLanguageModel, tools []types.Tool) []*provider.StreamChunk {
	t.Helper()
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var parts []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err != nil {
			return parts
		}
		parts = append(parts, chunk)
	}
}

func portprovbSSE(w http.ResponseWriter, events ...string) {
	for _, e := range events {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", e)
	}
}

// TS 'should keep the search arguments when the tool call input is parsed'.
func TestPortprovbXSearchKeepsArgumentsGenerate(t *testing.T) {
	model := portprovbModel(t, "application/json", func(w http.ResponseWriter) {
		_, _ = fmt.Fprintf(w, `{"id":"resp_123","object":"response","status":"completed","model":"grok-4-fast-non-reasoning","output":[{"type":"custom_tool_call","id":"ctc_123","call_id":"xs_call_123","name":"x_keyword_search","input":%q,"status":"completed"}],"usage":{"input_tokens":10,"output_tokens":5}}`, portprovbSearchArgumentsJSON)
	})
	tool := XSearch(XSearchConfig{})
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}, Tools: []types.Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v", result.ToolCalls)
	}
	parsed := portprovbParse(t, result.ToolCalls[0], tool)
	if !reflect.DeepEqual(parsed.Arguments, portprovbSearchArguments) {
		t.Fatalf("arguments = %#v", parsed.Arguments)
	}
}

// TS 'should keep the search arguments when the streamed tool call input is parsed'.
func TestPortprovbXSearchKeepsArgumentsStream(t *testing.T) {
	item := `{"type":"custom_tool_call","id":"ctc_123","call_id":"xs_call_123","name":"x_keyword_search"`
	model := portprovbModel(t, "text/event-stream", func(w http.ResponseWriter) {
		portprovbSSE(w,
			`{"type":"response.created","response":{"id":"resp_123","object":"response","model":"grok-4-fast-non-reasoning","status":"in_progress","output":[]}}`,
			`{"type":"response.output_item.added","item":`+item+`,"input":"","status":"in_progress"},"output_index":0}`,
			fmt.Sprintf(`{"type":"response.custom_tool_call_input.delta","item_id":"ctc_123","output_index":0,"delta":%q}`, portprovbSearchArgumentsJSON),
			fmt.Sprintf(`{"type":"response.custom_tool_call_input.done","item_id":"ctc_123","output_index":0,"input":%q}`, portprovbSearchArgumentsJSON),
			fmt.Sprintf(`{"type":"response.output_item.done","item":%s,"input":%q,"status":"completed"},"output_index":0}`, item, portprovbSearchArgumentsJSON),
			`{"type":"response.completed","response":{"id":"resp_123","object":"response","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":5}}}`,
		)
	})
	tool := XSearch(XSearchConfig{})
	var call *types.ToolCall
	for _, part := range portprovbStreamParts(t, model, []types.Tool{tool}) {
		if part.Type == provider.ChunkTypeToolCall {
			call = part.ToolCall
		}
	}
	if call == nil {
		t.Fatal("no tool call")
	}
	parsed := portprovbParse(t, *call, tool)
	if !reflect.DeepEqual(parsed.Arguments, portprovbSearchArguments) {
		t.Fatalf("arguments = %#v", parsed.Arguments)
	}
}

const portprovbMCPCall = `{"id":"mcp_1_call-0","type":"mcp_call","status":"completed","name":"read_wiki_structure","server_label":"deepwiki","error":"","arguments":"{\"repoName\":\"vercel/ai\"}","output":"{}"}`

// TS 'should keep the mcp arguments when the tool call input is parsed'.
func TestPortprovbMCPKeepsArgumentsGenerate(t *testing.T) {
	model := portprovbModel(t, "application/json", func(w http.ResponseWriter) {
		_, _ = fmt.Fprintf(w, `{"id":"r1","object":"response","status":"completed","model":"grok-4.7","output":[%s],"usage":{"input_tokens":1,"output_tokens":1}}`, portprovbMCPCall)
	})
	tool := MCPServer(MCPServerConfig{ServerURL: "https://mcp.deepwiki.com/mcp", ServerLabel: "deepwiki"})
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}, Tools: []types.Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].RawArguments != portprovbMCPArgumentsJSON || !result.ToolCalls[0].ProviderExecuted {
		t.Fatalf("tool calls = %#v", result.ToolCalls)
	}
	parsed := portprovbParse(t, result.ToolCalls[0], tool)
	if !reflect.DeepEqual(parsed.Arguments, map[string]interface{}{"repoName": "vercel/ai"}) {
		t.Fatalf("arguments = %#v", parsed.Arguments)
	}
}

// TS 'should stream the mcp tool call with its arguments': the tool call is
// emitted when the item is done, so the input start/delta/end carry the
// arguments (not the empty string of the added item).
func TestPortprovbMCPStreamEmitsToolCallWithArguments(t *testing.T) {
	added := `{"id":"mcp_1_call-0","type":"mcp_call","status":"in_progress","name":"read_wiki_structure","server_label":"deepwiki","arguments":""}`
	model := portprovbModel(t, "text/event-stream", func(w http.ResponseWriter) {
		portprovbSSE(w,
			`{"sequence_number":0,"type":"response.created","response":{"id":"r1","object":"response","model":"grok-4.7","status":"in_progress","output":[]}}`,
			`{"sequence_number":4,"type":"response.output_item.added","item":`+added+`,"output_index":1}`,
			`{"sequence_number":5,"type":"response.mcp_call.in_progress","item_id":"mcp_1_call-0","output_index":1}`,
			`{"sequence_number":6,"type":"response.mcp_call_arguments.delta","item_id":"mcp_1_call-0","output_index":1,"delta":"{\"repoName\":\"vercel/ai\"}"}`,
			`{"sequence_number":7,"type":"response.mcp_call_arguments.done","item_id":"mcp_1_call-0","output_index":1,"arguments":"{\"repoName\":\"vercel/ai\"}"}`,
			`{"sequence_number":8,"type":"response.mcp_call.completed","item_id":"mcp_1_call-0","output_index":1}`,
			`{"sequence_number":9,"type":"response.output_item.done","item":`+portprovbMCPCall+`,"output_index":1}`,
			`{"sequence_number":41,"type":"response.completed","response":{"id":"r1","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`,
		)
	})
	tool := MCPServer(MCPServerConfig{ServerURL: "https://mcp.deepwiki.com/mcp", ServerLabel: "deepwiki"})
	var kinds []provider.ChunkType
	var delta string
	var call *types.ToolCall
	for _, part := range portprovbStreamParts(t, model, []types.Tool{tool}) {
		switch part.Type {
		case provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputDelta, provider.ChunkTypeToolInputEnd, provider.ChunkTypeToolCall, provider.ChunkTypeToolResult:
			kinds = append(kinds, part.Type)
		}
		if part.Type == provider.ChunkTypeToolInputDelta {
			delta = part.Text
		}
		if part.Type == provider.ChunkTypeToolCall {
			call = part.ToolCall
		}
	}
	want := []provider.ChunkType{provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputDelta, provider.ChunkTypeToolInputEnd, provider.ChunkTypeToolCall, provider.ChunkTypeToolResult}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("parts = %v, want %v", kinds, want)
	}
	if delta != portprovbMCPArgumentsJSON {
		t.Fatalf("delta = %q", delta)
	}
	if call == nil || call.RawArguments != portprovbMCPArgumentsJSON || !call.ProviderExecuted {
		t.Fatalf("call = %#v", call)
	}
	parsed := portprovbParse(t, *call, tool)
	if !reflect.DeepEqual(parsed.Arguments, map[string]interface{}{"repoName": "vercel/ai"}) {
		t.Fatalf("arguments = %#v", parsed.Arguments)
	}
}
