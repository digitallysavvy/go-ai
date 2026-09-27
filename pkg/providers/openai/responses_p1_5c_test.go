package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// ── P1-5c item 1: new Responses output item decode (generate) ──────────────

func TestResponsesLanguageModel_ImageGenerationCallDecodesAsToolCallAndResult(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	item, _ := json.Marshal(map[string]interface{}{
		"type": "image_generation_call", "id": "ig_1", "result": "base64img",
	})
	result, err := model.convertResponse(mockResponsesResponseWith(item), true, "", nil, nil)
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ID != "ig_1" || result.ToolCalls[0].ToolName != "image_generation" {
		t.Fatalf("ToolCalls = %#v, want one image_generation call (TS toolNameMapping bare-name fallback)", result.ToolCalls)
	}
	if !result.ToolCalls[0].ProviderExecuted {
		t.Fatalf("ToolCalls[0].ProviderExecuted = false, want true")
	}
	var tr *types.ToolResultContent
	for _, c := range result.Content {
		if v, ok := c.(types.ToolResultContent); ok {
			tr = &v
		}
	}
	if tr == nil || tr.Result.(map[string]interface{})["result"] != "base64img" {
		t.Fatalf("tool result = %#v, want result=base64img", tr)
	}
}

func TestResponsesLanguageModel_FileSearchCallDecodesAsToolCallAndResult(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	item, _ := json.Marshal(map[string]interface{}{
		"type": "file_search_call", "id": "fs_1",
		"queries": []string{"q1"},
		"results": []map[string]interface{}{
			{"attributes": map[string]interface{}{"a": "b"}, "file_id": "file_1", "filename": "a.txt", "score": 0.9, "text": "hello"},
		},
	})
	result, err := model.convertResponse(mockResponsesResponseWith(item), true, "", nil, nil)
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ToolName != "file_search" {
		t.Fatalf("ToolCalls = %#v, want one file_search call (TS toolNameMapping bare-name fallback)", result.ToolCalls)
	}
	var tr *types.ToolResultContent
	for _, c := range result.Content {
		if v, ok := c.(types.ToolResultContent); ok {
			tr = &v
		}
	}
	if tr == nil {
		t.Fatalf("no tool result content found")
	}
	res := tr.Result.(map[string]interface{})
	results, ok := res["results"].([]map[string]interface{})
	if !ok || len(results) != 1 || results[0]["fileId"] != "file_1" {
		t.Fatalf("results = %#v, want fileId file_1", res["results"])
	}
}

func TestResponsesLanguageModel_CodeInterpreterCallDecodesAsToolCallAndResult(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	code := "print(1)"
	item, _ := json.Marshal(map[string]interface{}{
		"type": "code_interpreter_call", "id": "ci_1", "container_id": "cntr_1", "code": code, "status": "completed",
		"outputs": []map[string]interface{}{{"type": "logs", "logs": "1\n"}},
	})
	result, err := model.convertResponse(mockResponsesResponseWith(item), true, "", nil, nil)
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ToolName != "code_interpreter" {
		t.Fatalf("ToolCalls = %#v, want one code_interpreter call (TS toolNameMapping bare-name fallback)", result.ToolCalls)
	}
	if result.ToolCalls[0].Arguments["containerId"] != "cntr_1" || result.ToolCalls[0].Arguments["code"] != code {
		t.Fatalf("arguments = %#v, want containerId/code", result.ToolCalls[0].Arguments)
	}
}

func TestResponsesLanguageModel_ToolSearchHostedCallOutputPairing(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	callItem, _ := json.Marshal(map[string]interface{}{
		"type": "tool_search_call", "id": "tsc_1", "execution": "server", "call_id": nil, "status": "completed", "arguments": map[string]interface{}{"paths": []string{"foo"}},
	})
	outputItem, _ := json.Marshal(map[string]interface{}{
		"type": "tool_search_output", "id": "tso_1", "execution": "server", "call_id": nil, "status": "completed",
		"tools": []map[string]interface{}{{"type": "function", "name": "foo"}},
	})
	resp := mockResponsesResponseWith(callItem)
	resp.Output = append(resp.Output, outputItem)
	result, err := model.convertResponse(resp, true, "", nil, nil)
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if len(result.ToolCalls) != 1 || !result.ToolCalls[0].ProviderExecuted {
		t.Fatalf("ToolCalls = %#v, want one hosted (providerExecuted) tool_search call", result.ToolCalls)
	}
	callID := result.ToolCalls[0].ID
	if callID == "" {
		t.Fatalf("hosted tool_search call has empty id")
	}
	var tr *types.ToolResultContent
	for _, c := range result.Content {
		if v, ok := c.(types.ToolResultContent); ok {
			tr = &v
		}
	}
	if tr == nil || tr.ToolCallID != callID {
		t.Fatalf("tool_search_output ToolCallID = %#v, want it paired with hosted call id %q", tr, callID)
	}
}

func TestResponsesLanguageModel_McpCallDecodesAsToolCallAndResult(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	item, _ := json.Marshal(map[string]interface{}{
		"type": "mcp_call", "id": "mcp_1", "status": "completed", "arguments": `{"x":1}`,
		"name": "search", "server_label": "docs", "output": "found it",
	})
	result, err := model.convertResponse(mockResponsesResponseWith(item), true, "", nil, nil)
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ToolName != "mcp.search" || !result.ToolCalls[0].Dynamic {
		t.Fatalf("ToolCalls = %#v, want one dynamic mcp.search call", result.ToolCalls)
	}
	var tr *types.ToolResultContent
	for _, c := range result.Content {
		if v, ok := c.(types.ToolResultContent); ok {
			tr = &v
		}
	}
	if tr == nil {
		t.Fatalf("no tool result content found")
	}
	res := tr.Result.(map[string]interface{})
	if res["output"] != "found it" || res["serverLabel"] != "docs" {
		t.Fatalf("mcp result = %#v, want output=found it serverLabel=docs", res)
	}
}

func TestResponsesLanguageModel_McpApprovalRequestDecodesAsApprovalRequest(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	item, _ := json.Marshal(map[string]interface{}{
		"type": "mcp_approval_request", "id": "mar_1", "server_label": "docs", "name": "search", "arguments": `{}`,
	})
	result, err := model.convertResponse(mockResponsesResponseWith(item), true, "", nil, nil)
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if len(result.ToolCalls) != 1 || !result.ToolCalls[0].ProviderExecuted {
		t.Fatalf("ToolCalls = %#v, want one provider-executed mcp call", result.ToolCalls)
	}
	var approval *types.ToolApprovalRequestContent
	for _, c := range result.Content {
		if v, ok := c.(types.ToolApprovalRequestContent); ok {
			approval = &v
		}
	}
	if approval == nil || approval.ApprovalID != "mar_1" || approval.ToolCallID != result.ToolCalls[0].ID {
		t.Fatalf("approval = %#v, want approvalId mar_1 matching tool call id %q", approval, result.ToolCalls[0].ID)
	}
}

func TestResponsesLanguageModel_McpListToolsSkipped(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	item, _ := json.Marshal(map[string]interface{}{
		"type": "mcp_list_tools", "id": "mlt_1", "server_label": "docs",
		"tools": []map[string]interface{}{{"name": "search"}},
	})
	result, err := model.convertResponse(mockResponsesResponseWith(item), true, "", nil, nil)
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if len(result.ToolCalls) != 0 || len(result.Content) != 0 {
		t.Fatalf("result = %#v, want mcp_list_tools skipped entirely", result)
	}
}

// ── P1-5c item 2: annotations/citations -> source content ──────────────────

func TestResponsesLanguageModel_MessageAnnotationsDecodeAsSourceContent(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	msg, _ := json.Marshal(map[string]interface{}{
		"type": "message", "id": "msg_1", "role": "assistant",
		"content": []map[string]interface{}{
			{
				"type": "output_text", "text": "see the source",
				"annotations": []map[string]interface{}{
					{"type": "url_citation", "url": "https://example.com", "title": "Example", "start_index": 0, "end_index": 1},
					{"type": "file_citation", "file_id": "file_1", "filename": "a.txt", "index": 2},
				},
			},
		},
	})
	result, err := model.convertResponse(mockResponsesResponseWith(msg), true, "", nil, nil)
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	var sources []types.SourceContent
	for _, c := range result.Content {
		if v, ok := c.(types.SourceContent); ok {
			sources = append(sources, v)
		}
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %#v, want 2 source content parts", sources)
	}
	if sources[0].SourceType != "url" || sources[0].URL != "https://example.com" || sources[0].ID == "" {
		t.Fatalf("sources[0] = %#v, want a url source", sources[0])
	}
	if sources[1].SourceType != "document" || sources[1].Filename != "a.txt" {
		t.Fatalf("sources[1] = %#v, want a document source", sources[1])
	}
}

// mockResponsesResponseWith wraps a single raw output item in a minimal
// valid ResponsesAPIResponse.
func mockResponsesResponseWith(item json.RawMessage) responses.ResponsesAPIResponse {
	return responses.ResponsesAPIResponse{
		ID:     "resp_1",
		Model:  "gpt-4o",
		Output: []json.RawMessage{item},
		Usage:  &responses.ResponsesAPIUsage{InputTokens: 1, OutputTokens: 1},
	}
}

// ── P1-5c item 1: streaming decode ──────────────────────────────────────────

func TestResponsesLanguageModel_StreamFileSearchCallToolCallThenResult(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"file_search_call","id":"fs_1"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"file_search_call","id":"fs_1","queries":["q"],"results":null}}

`)), false)
	defer stream.Close() //nolint:errcheck

	call, err := stream.Next()
	if err != nil || call.Type != provider.ChunkTypeToolCall || call.ToolCall.ToolName != "file_search" {
		t.Fatalf("chunk = %#v, err = %v, want a file_search tool-call", call, err)
	}
	result, err := stream.Next()
	if err != nil || result.Type != provider.ChunkTypeToolResult || result.ToolResult.ToolCallID != "fs_1" {
		t.Fatalf("chunk = %#v, err = %v, want a file_search tool-result", result, err)
	}
}

func TestResponsesLanguageModel_StreamImageGenerationPartialImageThenResult(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"image_generation_call","id":"ig_1"}}

data: {"type":"response.image_generation_call.partial_image","item_id":"ig_1","output_index":0,"partial_image_b64":"partial"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"image_generation_call","id":"ig_1","result":"final"}}

`)), false)
	defer stream.Close() //nolint:errcheck

	call, err := stream.Next()
	if err != nil || call.Type != provider.ChunkTypeToolCall {
		t.Fatalf("chunk = %#v, err = %v, want image_generation tool-call", call, err)
	}
	partial, err := stream.Next()
	if err != nil || partial.Type != provider.ChunkTypeToolResult || !partial.ToolResult.Preliminary {
		t.Fatalf("chunk = %#v, err = %v, want a preliminary tool-result", partial, err)
	}
	if partial.ToolResult.Result.(map[string]interface{})["result"] != "partial" {
		t.Fatalf("partial result = %#v, want result=partial", partial.ToolResult.Result)
	}
	final, err := stream.Next()
	if err != nil || final.Type != provider.ChunkTypeToolResult || final.ToolResult.Preliminary {
		t.Fatalf("chunk = %#v, err = %v, want a final (non-preliminary) tool-result", final, err)
	}
}

func TestResponsesLanguageModel_StreamCodeInterpreterProgressiveInput(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"code_interpreter_call","id":"ci_1","container_id":"cntr_1"}}

data: {"type":"response.code_interpreter_call_code.delta","item_id":"ci_1","output_index":0,"delta":"print(1)"}

data: {"type":"response.code_interpreter_call_code.done","item_id":"ci_1","output_index":0,"code":"print(1)"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"code_interpreter_call","id":"ci_1","container_id":"cntr_1","code":"print(1)","outputs":[{"type":"logs","logs":"1\n"}],"status":"completed"}}

`)), false)
	defer stream.Close() //nolint:errcheck

	var chunkTypes []provider.ChunkType
	for i := 0; i < 6; i++ {
		c, err := stream.Next()
		if err != nil {
			t.Fatalf("Next() #%d error = %v", i, err)
		}
		chunkTypes = append(chunkTypes, c.Type)
		if c.Type == provider.ChunkTypeToolResult {
			break
		}
	}
	want := []provider.ChunkType{
		provider.ChunkTypeToolInputStart,
		provider.ChunkTypeToolInputDelta, // opening prefix
		provider.ChunkTypeToolInputDelta, // code delta
		provider.ChunkTypeToolInputDelta, // closing quote
		provider.ChunkTypeToolInputEnd,
		provider.ChunkTypeToolCall,
	}
	if len(chunkTypes) < len(want) {
		t.Fatalf("chunk types = %#v, want at least %#v", chunkTypes, want)
	}
	for i, w := range want {
		if chunkTypes[i] != w {
			t.Fatalf("chunk[%d] type = %v, want %v (full sequence %#v)", i, chunkTypes[i], w, chunkTypes)
		}
	}
}

func TestResponsesLanguageModel_StreamMcpCallToolCallAndResult(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"mcp_call","id":"mcp_1"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"mcp_call","id":"mcp_1","status":"completed","arguments":"{}","name":"search","server_label":"docs","output":"found"}}

`)), false)
	defer stream.Close() //nolint:errcheck

	call, err := stream.Next()
	if err != nil || call.Type != provider.ChunkTypeToolCall || call.ToolCall.ToolName != "mcp.search" || !call.ToolCall.Dynamic {
		t.Fatalf("chunk = %#v, err = %v, want a dynamic mcp.search tool-call", call, err)
	}
	result, err := stream.Next()
	if err != nil || result.Type != provider.ChunkTypeToolResult {
		t.Fatalf("chunk = %#v, err = %v, want an mcp tool-result", result, err)
	}
}

func TestResponsesLanguageModel_StreamMcpApprovalRequestEmitsApproval(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"mcp_approval_request","id":"mar_1"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"mcp_approval_request","id":"mar_1","server_label":"docs","name":"search","arguments":"{}"}}

`)), false)
	defer stream.Close() //nolint:errcheck

	call, err := stream.Next()
	if err != nil || call.Type != provider.ChunkTypeToolCall || !call.ToolCall.ProviderExecuted {
		t.Fatalf("chunk = %#v, err = %v, want a provider-executed mcp tool-call", call, err)
	}
	approval, err := stream.Next()
	if err != nil || approval.Type != provider.ChunkTypeToolApprovalRequest {
		t.Fatalf("chunk = %#v, err = %v, want a tool-approval-request", approval, err)
	}
	if approval.ToolApprovalRequest.ApprovalID != "mar_1" || approval.ToolApprovalRequest.ToolCallID != call.ToolCall.ID {
		t.Fatalf("approval = %#v, want approvalId mar_1 matching tool call id %q", approval.ToolApprovalRequest, call.ToolCall.ID)
	}
}

// ── P1-5c item 2 (stream): annotation added -> source chunk ─────────────────

func TestResponsesLanguageModel_StreamAnnotationAddedEmitsSource(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_text.annotation.added","annotation":{"type":"url_citation","start_index":0,"end_index":1,"url":"https://example.com","title":"Example"}}

`)), false)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeSource || chunk.SourceContent == nil {
		t.Fatalf("chunk = %#v, err = %v, want a source chunk", chunk, err)
	}
	if chunk.SourceContent.URL != "https://example.com" || chunk.SourceContent.SourceType != "url" {
		t.Fatalf("source = %#v, want url https://example.com", chunk.SourceContent)
	}
}

// ── P1-5c item 3: reasoning summary-part streaming boundaries ──────────────

func TestResponsesLanguageModel_StreamReasoningMultiSummaryBoundaries(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}

data: {"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"first"}

data: {"type":"response.reasoning_summary_part.done","item_id":"rs_1","output_index":0,"summary_index":0}

data: {"type":"response.reasoning_summary_part.added","item_id":"rs_1","output_index":0,"summary_index":1}

data: {"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":1,"delta":"second"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","encrypted_content":"enc"}}

`)), false)
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for i := 0; i < 10; i++ {
		c, err := stream.Next()
		if err != nil {
			t.Fatalf("Next() #%d error = %v", i, err)
		}
		chunks = append(chunks, c)
		if len(chunks) >= 2 && c.Type == provider.ChunkTypeReasoningEnd {
			// Keep draining until the summary-index-1 block also closes.
			if c.ID == "rs_1:1" {
				break
			}
		}
	}

	// store defaults to false (not set on the stream in this test), so the
	// summary_index=0 part.done should NOT immediately close it -- it
	// should only close once summary_index=1's reasoning-start needs to
	// open (can-conclude -> concluded).
	var gotStart0, gotDelta0, gotStart1, gotDelta1, gotEnd0, gotEnd1 bool
	for _, c := range chunks {
		switch {
		case c.Type == provider.ChunkTypeReasoningStart && c.ID == "rs_1:0":
			gotStart0 = true
		case c.Type == provider.ChunkTypeReasoning && c.ID == "rs_1:0":
			gotDelta0 = true
		case c.Type == provider.ChunkTypeReasoningStart && c.ID == "rs_1:1":
			gotStart1 = true
		case c.Type == provider.ChunkTypeReasoning && c.ID == "rs_1:1":
			gotDelta1 = true
		case c.Type == provider.ChunkTypeReasoningEnd && c.ID == "rs_1:0":
			gotEnd0 = true
		case c.Type == provider.ChunkTypeReasoningEnd && c.ID == "rs_1:1":
			gotEnd1 = true
		}
	}
	if !gotStart0 || !gotDelta0 || !gotStart1 || !gotDelta1 || !gotEnd0 || !gotEnd1 {
		t.Fatalf("chunks = %#v, missing expected reasoning boundary/delta chunks (start0=%v delta0=%v start1=%v delta1=%v end0=%v end1=%v)",
			chunks, gotStart0, gotDelta0, gotStart1, gotDelta1, gotEnd0, gotEnd1)
	}
	// end0 must be emitted before start1 opens the next block is not
	// required by TS ordering (end0 is emitted as part of handling
	// summary_index=1's "added" event, right before start1), but end0 must
	// come before end1.
	idxEnd0, idxEnd1 := -1, -1
	for i, c := range chunks {
		if c.Type == provider.ChunkTypeReasoningEnd && c.ID == "rs_1:0" {
			idxEnd0 = i
		}
		if c.Type == provider.ChunkTypeReasoningEnd && c.ID == "rs_1:1" {
			idxEnd1 = i
		}
	}
	if idxEnd0 == -1 || idxEnd1 == -1 || idxEnd0 >= idxEnd1 {
		t.Fatalf("chunks = %#v, want end0 before end1", chunks)
	}
}

// ── P1-5c item 9: progressive tool-input-delta for custom_tool_call ────────

func TestResponsesLanguageModel_StreamCustomToolCallProgressiveInput(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"format_code"}}

data: {"type":"response.custom_tool_call_input.delta","item_id":"ctc_1","output_index":0,"delta":"prin"}

data: {"type":"response.custom_tool_call_input.delta","item_id":"ctc_1","output_index":0,"delta":"t(1)"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"format_code","input":"print(1)"}}

`)), false)
	defer stream.Close() //nolint:errcheck

	start, err := stream.Next()
	if err != nil || start.Type != provider.ChunkTypeToolInputStart || start.ToolCall.ID != "call_1" || start.ToolCall.ToolName != "format_code" {
		t.Fatalf("chunk = %#v, err = %v, want a format_code tool-input-start", start, err)
	}
	delta1, err := stream.Next()
	if err != nil || delta1.Type != provider.ChunkTypeToolInputDelta || delta1.ID != "call_1" || delta1.Text != "prin" {
		t.Fatalf("chunk = %#v, err = %v, want tool-input-delta \"prin\"", delta1, err)
	}
	delta2, err := stream.Next()
	if err != nil || delta2.Type != provider.ChunkTypeToolInputDelta || delta2.Text != "t(1)" {
		t.Fatalf("chunk = %#v, err = %v, want tool-input-delta \"t(1)\"", delta2, err)
	}
	end, err := stream.Next()
	if err != nil || end.Type != provider.ChunkTypeToolInputEnd || end.ToolCall.ID != "call_1" {
		t.Fatalf("chunk = %#v, err = %v, want tool-input-end", end, err)
	}
	call, err := stream.Next()
	if err != nil || call.Type != provider.ChunkTypeToolCall || call.ToolCall.ToolName != "format_code" {
		t.Fatalf("chunk = %#v, err = %v, want the final tool-call", call, err)
	}
	if call.ToolCall.Arguments["input"] != "print(1)" {
		t.Fatalf("tool call arguments = %#v, want input=print(1)", call.ToolCall.Arguments)
	}
}

func TestResponsesLanguageModel_StreamReasoningStoreTrueClosesImmediately(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}

data: {"type":"response.reasoning_summary_part.done","item_id":"rs_1","output_index":0,"summary_index":0}

`)), false)
	stream.store = true
	defer stream.Close() //nolint:errcheck

	start, err := stream.Next()
	if err != nil || start.Type != provider.ChunkTypeReasoningStart {
		t.Fatalf("chunk = %#v, err = %v, want reasoning-start", start, err)
	}
	end, err := stream.Next()
	if err != nil || end.Type != provider.ChunkTypeReasoningEnd || end.ID != "rs_1:0" {
		t.Fatalf("chunk = %#v, err = %v, want an immediate reasoning-end (store=true)", end, err)
	}
}

// ── MCP approvalRequestIdToDummyToolCallIdFromPrompt (P1-5c follow-up) ─────
//
// Mirrors TS's extractApprovalRequestIdToToolCallIdMapping: when an MCP tool
// call was approved in a PREVIOUS turn, the approval request id is carried
// in the current turn's prompt on the assistant tool-call part that
// represented the pending approval (providerOptions.openai.approvalRequestId
// -> that part's toolCallId). A later mcp_call in a new response that shares
// the same approval_request_id must reuse that same tool call id.

func TestResponsesLanguageModel_McpCallUsesApprovalIDFromPromptGenerate(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	prompt := types.Prompt{Messages: []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ToolCallContent{
					ToolCallID: "dummy_call_1",
					ToolName:   "mcp.search",
					ProviderOptions: map[string]interface{}{
						"openai": map[string]interface{}{"approvalRequestId": "req_1"},
					},
				},
			},
		},
	}}
	approvalFromPrompt := extractApprovalRequestIDToToolCallIDFromPrompt(prompt)
	if approvalFromPrompt["req_1"] != "dummy_call_1" {
		t.Fatalf("approvalFromPrompt = %#v, want req_1 -> dummy_call_1", approvalFromPrompt)
	}

	item, _ := json.Marshal(map[string]interface{}{
		"type": "mcp_call", "id": "mcp_2", "status": "completed", "arguments": `{"x":1}`,
		"name": "search", "server_label": "docs", "output": "found it",
		"approval_request_id": "req_1",
	})
	result, err := model.convertResponse(mockResponsesResponseWith(item), true, "", nil, approvalFromPrompt)
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ID != "dummy_call_1" {
		t.Fatalf("ToolCalls = %#v, want id aliased to dummy_call_1 from the prompt", result.ToolCalls)
	}
	var tc types.ToolCallContent
	var tr types.ToolResultContent
	for _, c := range result.Content {
		switch v := c.(type) {
		case types.ToolCallContent:
			tc = v
		case types.ToolResultContent:
			tr = v
		}
	}
	if tc.ToolCallID != "dummy_call_1" || tr.ToolCallID != "dummy_call_1" {
		t.Fatalf("content tool-call/result ids = %q/%q, want dummy_call_1", tc.ToolCallID, tr.ToolCallID)
	}
}

func TestResponsesLanguageModel_McpCallWithUnknownApprovalIDFallsBackToItemID(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	item, _ := json.Marshal(map[string]interface{}{
		"type": "mcp_call", "id": "mcp_3", "status": "completed", "arguments": `{}`,
		"name": "search", "server_label": "docs", "output": "found it",
		"approval_request_id": "req_unknown",
	})
	result, err := model.convertResponse(mockResponsesResponseWith(item), true, "", nil, map[string]string{})
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ID != "mcp_3" {
		t.Fatalf("ToolCalls = %#v, want fallback to item id mcp_3", result.ToolCalls)
	}
}

func TestResponsesLanguageModel_StreamMcpCallUsesApprovalIDFromPrompt(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"mcp_call","id":"mcp_1"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"mcp_call","id":"mcp_1","status":"completed","arguments":"{}","name":"search","server_label":"docs","output":"found","approval_request_id":"req_1"}}

`)), false)
	stream.approvalFromPrompt = map[string]string{"req_1": "dummy_call_1"}
	defer stream.Close() //nolint:errcheck

	call, err := stream.Next()
	if err != nil || call.Type != provider.ChunkTypeToolCall || call.ToolCall.ID != "dummy_call_1" {
		t.Fatalf("chunk = %#v, err = %v, want tool-call aliased to dummy_call_1 from the prompt", call, err)
	}
	result, err := stream.Next()
	if err != nil || result.Type != provider.ChunkTypeToolResult || result.ToolResult.ToolCallID != "dummy_call_1" {
		t.Fatalf("chunk = %#v, err = %v, want tool-result aliased to dummy_call_1", result, err)
	}
}

func TestResponsesLanguageModel_StreamMcpCallStreamAliasTakesPrecedenceOverPrompt(t *testing.T) {
	// Both an in-stream mcp_approval_request (which populates
	// mcpApprovalAlias) and a prompt-derived alias exist for the same
	// approval_request_id; the in-stream one (the more recent) wins, as in
	// TS: `approvalRequestIdToDummyToolCallIdFromStream.get(id) ??
	// approvalRequestIdToDummyToolCallIdFromPrompt[id] ?? value.item.id`.
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"mcp_approval_request","id":"mar_1"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"mcp_approval_request","id":"mar_1","server_label":"docs","name":"search","arguments":"{}","approval_request_id":"req_1"}}

data: {"type":"response.output_item.added","output_index":1,"item":{"type":"mcp_call","id":"mcp_1"}}

data: {"type":"response.output_item.done","output_index":1,"item":{"type":"mcp_call","id":"mcp_1","status":"completed","arguments":"{}","name":"search","server_label":"docs","output":"found","approval_request_id":"req_1"}}

`)), false)
	stream.approvalFromPrompt = map[string]string{"req_1": "stale_dummy_call"}
	defer stream.Close() //nolint:errcheck

	approvalCall, err := stream.Next()
	if err != nil || approvalCall.Type != provider.ChunkTypeToolCall {
		t.Fatalf("chunk = %#v, err = %v, want the approval-request's tool-call", approvalCall, err)
	}
	streamDummyID := approvalCall.ToolCall.ID
	if streamDummyID == "stale_dummy_call" {
		t.Fatalf("stream-generated dummy id unexpectedly equals the prompt-derived one")
	}
	approval, err := stream.Next()
	if err != nil || approval.Type != provider.ChunkTypeToolApprovalRequest {
		t.Fatalf("chunk = %#v, err = %v, want a tool-approval-request", approval, err)
	}
	call, err := stream.Next()
	if err != nil || call.Type != provider.ChunkTypeToolCall || call.ToolCall.ID != streamDummyID {
		t.Fatalf("chunk = %#v, err = %v, want mcp_call aliased to the in-stream dummy id %q (not the prompt one)", call, err, streamDummyID)
	}
}

// ── Reasoning summary-part follow-up fixes (post-review) ───────────────────

// TestResponsesLanguageModel_StreamReasoningSummaryIDRotation ports TS's
// "should correlate rotated item ids by output index"
// (github-copilot-id-rotation.1 fixture): every SSE event in a single
// reasoning block can carry a distinct item_id, but every resulting
// reasoning-* chunk must collapse to the id first seen at
// output_item.added. This must hold for reasoning_summary_part.added,
// reasoning_summary_text.delta, and reasoning_summary_part.done too, not
// just output_item.done.
func TestResponsesLanguageModel_StreamReasoningSummaryIDRotation(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"capture-id-3"}}

data: {"type":"response.reasoning_summary_part.added","item_id":"capture-id-4","output_index":0,"summary_index":0}

data: {"type":"response.reasoning_summary_text.delta","item_id":"capture-id-5","output_index":0,"summary_index":0,"delta":"thinking"}

data: {"type":"response.reasoning_summary_part.done","item_id":"capture-id-7","output_index":0,"summary_index":0}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"capture-id-8","encrypted_content":null,"summary":[{"type":"summary_text","text":"thinking"}]}}

`)), false)
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		chunks = append(chunks, c)
		if c.Type == provider.ChunkTypeReasoningEnd {
			break
		}
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks emitted")
	}
	for _, c := range chunks {
		if c.ID != "capture-id-3:0" {
			t.Fatalf("chunk %#v has id %q, want the first-seen id capture-id-3:0", c, c.ID)
		}
	}
}

// TestResponsesLanguageModel_ReasoningSummaryTextDeltaEmptyDeltaStillEmits
// covers finding (C): TS unconditionally enqueues reasoning-delta even for
// an empty-string delta -- there is no guard to skip it.
func TestResponsesLanguageModel_ReasoningSummaryTextDeltaEmptyDeltaStillEmits(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}

data: {"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":""}

`)), false)
	defer stream.Close() //nolint:errcheck

	start, err := stream.Next()
	if err != nil || start.Type != provider.ChunkTypeReasoningStart {
		t.Fatalf("chunk = %#v, err = %v, want reasoning-start", start, err)
	}
	delta, err := stream.Next()
	if err != nil || delta.Type != provider.ChunkTypeReasoning || delta.Reasoning != "" {
		t.Fatalf("chunk = %#v, err = %v, want an (empty-string) reasoning delta chunk, not a skip", delta, err)
	}
}

// TestResponsesExplicitStore covers finding (A): responsesExplicitStore
// must return false (deferring reasoning-end to output_item.done) whenever
// the caller did not explicitly set providerOptions.openai.store to a
// boolean -- matching TS's raw `openaiOptions?.store` truthy check, where
// `undefined` is falsy. Only an explicit `store: true` should return true.
func TestResponsesExplicitStore(t *testing.T) {
	tests := []struct {
		name string
		body map[string]interface{}
		want bool
	}{
		{"unset (no key)", map[string]interface{}{}, false},
		{"explicit true", map[string]interface{}{"store": true}, true},
		{"explicit false", map[string]interface{}{"store": false}, false},
		{"explicit null", map[string]interface{}{"store": nil}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := responsesExplicitStore(tt.body); got != tt.want {
				t.Fatalf("responsesExplicitStore(%#v) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

// TestResponsesLanguageModel_DoStreamDefaultStoreDefersReasoningEnd is an
// end-to-end regression test for finding (A) through the real DoStream path
// (not the newResponsesStream test helper, which defaults stream.store to
// Go's zero value and so could mask this bug): with no providerOptions.openai
// set at all, a reasoning summary part's .done must NOT immediately close
// the reasoning block -- it must defer to output_item.done, matching TS's
// default (store option unset behaves like store=false).
func TestResponsesLanguageModel_DoStreamDefaultStoreDefersReasoningEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		events := []string{
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}`,
			`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"thinking"}`,
			`{"type":"response.reasoning_summary_part.done","item_id":"rs_1","output_index":0,"summary_index":0}`,
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","encrypted_content":"enc","summary":[{"type":"summary_text","text":"thinking"}]}}`,
			`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":1}}}`,
		}
		for _, e := range events {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hi"}}},
		}},
	})
	if err != nil {
		t.Fatalf("DoStream failed: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	var sawReasoningEndBeforeDone bool
	var sawFinalReasoningEnd bool
	var finalMeta map[string]interface{}
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeReasoningEnd {
			if !sawFinalReasoningEnd {
				sawFinalReasoningEnd = true
				_ = json.Unmarshal(c.ProviderMetadata, &finalMeta)
			} else {
				sawReasoningEndBeforeDone = true
			}
		}
	}
	if !sawFinalReasoningEnd {
		t.Fatal("expected a reasoning-end chunk")
	}
	if sawReasoningEndBeforeDone {
		t.Fatal("got more than one reasoning-end chunk; the store=false-default path should defer to a single reasoning-end at output_item.done")
	}
	openaiMeta, _ := finalMeta["openai"].(map[string]interface{})
	if openaiMeta == nil || openaiMeta["reasoningEncryptedContent"] != "enc" {
		t.Fatalf("reasoning-end providerMetadata = %#v, want reasoningEncryptedContent \"enc\" from output_item.done (proves it was deferred, not closed early at reasoning_summary_part.done)", finalMeta)
	}
}

// TestConvertResponse_ReasoningMultipleSummaryPartsProducesSeparateContent
// covers finding (E): TS's doGenerate pushes one `reasoning` content entry
// per summary array element (openai-responses-language-model.ts), not one
// entry with every part's text concatenated together.
func TestConvertResponse_ReasoningMultipleSummaryPartsProducesSeparateContent(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	item, _ := json.Marshal(map[string]interface{}{
		"type": "reasoning", "id": "rs_1", "encrypted_content": "enc",
		"summary": []map[string]interface{}{
			{"type": "summary_text", "text": "first"},
			{"type": "summary_text", "text": "second"},
		},
	})
	result, err := model.convertResponse(mockResponsesResponseWith(item), true, "", nil, nil)
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	var reasoningParts []types.ReasoningContent
	for _, c := range result.Content {
		if rc, ok := c.(types.ReasoningContent); ok {
			reasoningParts = append(reasoningParts, rc)
		}
	}
	if len(reasoningParts) != 2 {
		t.Fatalf("reasoning content parts = %#v, want 2 separate blocks", reasoningParts)
	}
	if reasoningParts[0].Text != "first" || reasoningParts[1].Text != "second" {
		t.Fatalf("reasoning parts = %#v, want texts \"first\" then \"second\"", reasoningParts)
	}
	if reasoningParts[0].EncryptedContent != "enc" || reasoningParts[1].EncryptedContent != "enc" {
		t.Fatalf("reasoning parts = %#v, want both to carry the item's encrypted_content", reasoningParts)
	}
}

// ── Hosted-tool name resolution / providerExecuted follow-up fixes ─────────
//
// TS resolves a hosted tool's surfaced name via
// toolNameMapping.toCustomToolName(bareName): a caller-registered "provider"
// tool with a custom SDK Name wins; otherwise the bare, unprefixed provider
// tool identity (e.g. "image_generation") is used -- never the internal
// "openai.image_generation" id. These tests cover both the bare-name
// fallback (above) and the custom-name override (here), for both paths.

func TestResponsesLanguageModel_ImageGenerationCallUsesCustomToolName(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")
	customTools := []types.Tool{{Type: types.ToolTypeProviderDefined, Name: "generateImage", ProviderID: "openai.image_generation"}}

	item, _ := json.Marshal(map[string]interface{}{
		"type": "image_generation_call", "id": "ig_1", "result": "base64img",
	})
	result, err := model.convertResponse(mockResponsesResponseWith(item), true, "", customTools, nil)
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ToolName != "generateImage" {
		t.Fatalf("ToolCalls = %#v, want the custom name generateImage", result.ToolCalls)
	}
}

func TestResponsesLanguageModel_StreamImageGenerationCallUsesCustomToolName(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"image_generation_call","id":"ig_1"}}

`)), false)
	stream.tools = []types.Tool{{Type: types.ToolTypeProviderDefined, Name: "generateImage", ProviderID: "openai.image_generation"}}
	defer stream.Close() //nolint:errcheck

	call, err := stream.Next()
	if err != nil || call.Type != provider.ChunkTypeToolCall || call.ToolCall.ToolName != "generateImage" {
		t.Fatalf("chunk = %#v, err = %v, want tool-call named generateImage", call, err)
	}
}

// TestResponsesLanguageModel_HostedToolResultsHaveNoProviderExecuted covers
// finding #3: TS never sets providerExecuted on the tool-RESULT for
// image_generation_call/file_search_call/code_interpreter_call, only on the
// tool-call.
func TestResponsesLanguageModel_HostedToolResultsHaveNoProviderExecuted(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	items := []json.RawMessage{
		mustMarshal(map[string]interface{}{"type": "image_generation_call", "id": "ig_1", "result": "b64"}),
		mustMarshal(map[string]interface{}{"type": "file_search_call", "id": "fs_1", "queries": []string{"q"}}),
		mustMarshal(map[string]interface{}{"type": "code_interpreter_call", "id": "ci_1", "container_id": "c1", "code": "1", "status": "completed"}),
	}
	for _, item := range items {
		result, err := model.convertResponse(mockResponsesResponseWith(item), true, "", nil, nil)
		if err != nil {
			t.Fatalf("convertResponse failed: %v", err)
		}
		var sawResult bool
		for _, c := range result.Content {
			if tr, ok := c.(types.ToolResultContent); ok {
				sawResult = true
				if tr.ProviderExecuted {
					t.Fatalf("item %s: tool-result ProviderExecuted = true, want false (only the tool-call should carry it)", item)
				}
			}
		}
		if !sawResult {
			t.Fatalf("item %s: no tool-result content found", item)
		}
	}
}

// TestResponsesLanguageModel_CodeInterpreterCallNullCodeIncludesNullKey
// covers finding #4: TS's schema is `code: z.string().nullable()`, always
// present (null when absent) since JSON.stringify still includes a
// null-valued key; Go must include "code":null in the input JSON rather
// than omitting the key entirely.
func TestResponsesLanguageModel_CodeInterpreterCallNullCodeIncludesNullKey(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	item, _ := json.Marshal(map[string]interface{}{
		"type": "code_interpreter_call", "id": "ci_1", "container_id": "cntr_1", "status": "in_progress",
	})
	result, err := model.convertResponse(mockResponsesResponseWith(item), true, "", nil, nil)
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %#v, want one", result.ToolCalls)
	}
	if !strings.Contains(result.ToolCalls[0].RawArguments, `"code":null`) {
		t.Fatalf("RawArguments = %q, want it to include \"code\":null", result.ToolCalls[0].RawArguments)
	}
	codeVal, hasCode := result.ToolCalls[0].Arguments["code"]
	if !hasCode || codeVal != nil {
		t.Fatalf("Arguments[\"code\"] = %#v (present=%v), want present and nil", codeVal, hasCode)
	}
}

func mustMarshal(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
