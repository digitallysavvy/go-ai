package anthropic

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// r98provaStream runs SSE data frames through the stream parser and returns
// every chunk.
func r98provaStream(t *testing.T, usesJSONTool bool, tools []types.Tool, frames ...string) []*provider.StreamChunk {
	t.Helper()
	var b strings.Builder
	for _, f := range frames {
		b.WriteString("data: " + f + "\n\n")
	}
	s := newAnthropicStream(io.NopCloser(strings.NewReader(b.String())), usesJSONTool, tools)
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		out = append(out, c)
	}
	return out
}

const r98provaStart = `{"type":"message_start","message":{"id":"m1","model":"x","usage":{"input_tokens":1}}}`

// zod z.record walks every entry in key order and reports each entry's
// issues; Go reported one entry, picked by map iteration order.
func TestR98provaSafeguardToolUsesRecordIssues(t *testing.T) {
	frame := `{"type":"message_delta","delta":{"safeguard_results":[{"type":"t","status":{"type":"s","tool_uses":{"b":{"type":1},"a":{"type":2}}}}]},"usage":{"output_tokens":1}}`
	for i := 0; i < 20; i++ {
		msg := anthropicChunkValidationMessage(frame)
		var issues []struct {
			Path []interface{} `json:"path"`
		}
		if err := json.Unmarshal([]byte(validationIssueText(msg)), &issues); err != nil {
			t.Fatalf("issues %q: %v", msg, err)
		}
		var keys []string
		for _, is := range issues {
			if len(is.Path) >= 2 {
				if k, ok := is.Path[len(is.Path)-2].(string); ok {
					keys = append(keys, k)
				}
			}
		}
		if strings.Join(keys, ",") != "b,a" {
			t.Fatalf("issue entries = %v, want b,a (wire order, every entry): %s", keys, msg)
		}
	}
}

// TS serverToolCalls records only tool-search calls, so a
// tool_search_tool_result whose id belongs to another server tool falls back
// to the tool-search name (generate, batch and stream).
func TestR98provaToolSearchResultIgnoresOtherServerTools(t *testing.T) {
	var response anthropicResponse
	if err := jsonexact.Unmarshal([]byte(`{"type":"message","content":[
		{"type":"server_tool_use","id":"s1","name":"web_search","input":{"query":"q"}},
		{"type":"tool_search_tool_result","tool_use_id":"s1","content":{"type":"tool_search_tool_search_result","tool_references":[]}}
	],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`), &response); err != nil {
		t.Fatal(err)
	}
	m := &LanguageModel{provider: New(Config{APIKey: "k"})}
	result := m.convertResponseWithOptions(response, convertOptions{})
	var got []string
	for _, part := range result.Content {
		if tr, ok := part.(types.ToolResultContent); ok {
			got = append(got, tr.ToolName)
		}
	}
	if strings.Join(got, ",") != "tool_search_tool_regex" {
		t.Fatalf("generate tool result names = %v, want tool_search_tool_regex", got)
	}

	chunks := r98provaStream(t, false, nil, r98provaStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"s1","name":"web_search","input":{}}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_search_tool_result","tool_use_id":"s1","content":{"type":"tool_search_tool_search_result","tool_references":[]}}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_stop"}`)
	got = nil
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolResult {
			got = append(got, c.ToolResult.ToolName)
		}
	}
	if strings.Join(got, ",") != "tool_search_tool_regex" {
		t.Fatalf("stream tool result names = %v, want tool_search_tool_regex", got)
	}
}

// TS streams web_fetch / web_search tool results under
// toCustomToolName('web_fetch' / 'web_search'), whatever server tool the id
// belongs to.
func TestR98provaStreamWebToolResultNames(t *testing.T) {
	chunks := r98provaStream(t, false, nil, r98provaStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"s1","name":"web_search","input":{"query":"q"}}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"web_fetch_tool_result","tool_use_id":"s1","content":{"type":"web_fetch_tool_result_error","error_code":"x"}}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"server_tool_use","id":"s2","name":"web_fetch","input":{"url":"u"}}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"content_block_start","index":3,"content_block":{"type":"web_search_tool_result","tool_use_id":"s2","content":{"type":"web_search_tool_result_error","error_code":"x"}}}`,
		`{"type":"content_block_stop","index":3}`,
		`{"type":"message_stop"}`)
	var got []string
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolResult {
			got = append(got, c.ToolResult.ToolName)
		}
	}
	if strings.Join(got, ",") != "web_fetch,web_search" {
		t.Fatalf("tool result names = %v, want web_fetch,web_search", got)
	}
}

// TS JSON.stringify(part.input) of a streamed mcp_tool_use: an input that is
// not an object (z.unknown()) keeps its own text.
func TestR98provaStreamMCPToolUseNonObjectInput(t *testing.T) {
	chunks := r98provaStream(t, false, nil, r98provaStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"mcp_tool_use","id":"t1","name":"n","input":null,"server_name":"s"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"mcp_tool_use","id":"t2","name":"n","input":[1,"a"],"server_name":"s"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_stop"}`)
	var got []string
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolCall {
			got = append(got, c.ToolCall.RawArguments)
		}
	}
	if strings.Join(got, "|") != `null|[1,"a"]` {
		t.Fatalf("mcp inputs = %v, want null and [1,\"a\"]", got)
	}
}

// TS compares message ids strictly: an absent id (undefined) and a null id
// differ, so the second message_start is a spliced stream; two absent ids
// are the same message.
func TestR98provaSplicedStreamNullVersusAbsentID(t *testing.T) {
	chunks := r98provaStream(t, false, nil,
		`{"type":"message_start","message":{"usage":{"input_tokens":1}}}`,
		`{"type":"message_start","message":{"id":null,"usage":{"input_tokens":1}}}`)
	var errText string
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError {
			errText = c.Text
		}
	}
	if errText != "Received message_start for message null while message undefined is still open." {
		t.Fatalf("error = %q, want the spliced-stream error for null vs undefined", errText)
	}

	chunks = r98provaStream(t, false, nil,
		`{"type":"message_start","message":{"usage":{"input_tokens":1}}}`,
		`{"type":"message_start","message":{"usage":{"input_tokens":1}}}`)
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("absent ids twice gave an error: %q", c.Text)
		}
	}
}

// TS reads only delta.container on message_delta (zod strips a root-level
// container), and a delta without one resets the container to null.
func TestR98provaMessageDeltaRootContainerIgnored(t *testing.T) {
	chunks := r98provaStream(t, false, nil, r98provaStart,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1},"container":{"id":"c","expires_at":"e"}}`,
		`{"type":"message_stop"}`)
	for _, c := range chunks {
		if c.Type != provider.ChunkTypeFinish {
			continue
		}
		var meta struct {
			Anthropic map[string]interface{} `json:"anthropic"`
		}
		if err := json.Unmarshal(c.ProviderMetadata, &meta); err != nil {
			t.Fatal(err)
		}
		if v, ok := meta.Anthropic["container"]; !ok || v != nil {
			t.Fatalf("container = %#v, want null", v)
		}
		return
	}
	t.Fatal("no finish chunk")
}

// TS content_block_stop skips a tool-call block whose custom tool name is
// `json` while the JSON response tool is in use: no tool-input-end and no
// tool-call.
func TestR98provaStreamJSONNamedToolCallSuppressed(t *testing.T) {
	tools := []types.Tool{{Name: "json", Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.web_search_20250305"}}
	chunks := r98provaStream(t, true, tools, r98provaStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"s1","name":"web_search","input":{"query":"q"}}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_stop"}`)
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolCall || c.Type == provider.ChunkTypeToolInputEnd {
			t.Fatalf("unexpected %s chunk for the json-named tool block: %+v", c.Type, c.ToolCall)
		}
	}
}

// TS getFromApi appends getRuntimeEnvironmentUserAgent() to the results
// download (same-origin and off-origin hops alike; an off-origin hop sends
// no other header).
func TestR98provaBatchResultsRuntimeUserAgent(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	var mu sync.Mutex
	var ua string
	mux.HandleFunc("/messages/batches/b1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":0,"errored":0,"canceled":1,"expired":0},"created_at":"c","expires_at":"e","results_url":"` + srv.URL + `/results"}`))
	})
	mux.HandleFunc("/results", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ua = r.Header.Get("User-Agent")
		mu.Unlock()
		_, _ = w.Write([]byte(`{"custom_id":"x","result":{"type":"canceled"}}`))
	})
	b := New(Config{APIKey: "k", BaseURL: srv.URL}).ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
	mu.Lock()
	defer mu.Unlock()
	if !strings.HasSuffix(ua, " "+providerutils.RuntimeEnvironmentUserAgent()) {
		t.Fatalf("results User-Agent = %q, want it to end with the runtime tag", ua)
	}

	// The off-origin hop (not reachable through the SSRF transport in a
	// test) gets only the user agent.
	off := anthropicBatchResultsHeaders(nil)
	if len(off) != 1 || off["user-agent"] != providerutils.RuntimeEnvironmentUserAgent() {
		t.Fatalf("off-origin headers = %v, want only the runtime user agent", off)
	}
}
