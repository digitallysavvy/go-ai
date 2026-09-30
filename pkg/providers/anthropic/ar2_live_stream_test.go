package anthropic

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// AR2: live-streamed web_fetch_tool_result / web_search_tool_result content
// blocks were falling to the content_block_start default case and being
// dropped (no tool-result, no source chunks). Ports TS
// anthropic-language-model.ts doStream's content_block_start switch,
// "web_fetch_tool_result" (~line 2226) and "web_search_tool_result"
// (~line 2272) cases. Unlike TestStream_WebFetchCitationDocument (which
// covers the *pre-populated* message_start.message.content path), these
// tests cover the tool result arriving live via its own content_block_start
// event, which TS handles in the same switch as server_tool_use.

// TestStream_LiveWebFetchToolResult ports TS's content_block_start
// "web_fetch_tool_result" case: a live (not pre-populated) web_fetch result
// emits a provider-executed tool-result chunk with camelCased wire fields,
// and grows citationDocuments so a later page_location citation resolves.
func TestStream_LiveWebFetchToolResult(t *testing.T) {
	sseData := "" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"server_tool_use\",\"id\":\"srvtoolu_1\",\"name\":\"web_fetch\",\"input\":{}}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"web_fetch_tool_result\",\"tool_use_id\":\"srvtoolu_1\",\"content\":" +
		webFetchResultContent("Fetched Report", "application/pdf") + "}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)
	stream.citationDocuments = nil

	// First chunk: tool-input-start is not emitted for provider-executed
	// server tools (matches TS "tool-input-start" only for the caller-visible
	// tool_use case); the first real chunk here is the tool-call for
	// server_tool_use.
	callChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() tool-call error: %v", err)
	}
	if callChunk.Type != provider.ChunkTypeToolCall {
		t.Fatalf("chunk.Type = %v, want ChunkTypeToolCall", callChunk.Type)
	}

	resultChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() tool-result error: %v", err)
	}
	if resultChunk.Type != provider.ChunkTypeToolResult {
		t.Fatalf("chunk.Type = %v, want ChunkTypeToolResult", resultChunk.Type)
	}
	tr := resultChunk.ToolResult
	if tr == nil {
		t.Fatal("ToolResult is nil")
	}
	if tr.ToolCallID != "srvtoolu_1" {
		t.Errorf("ToolCallID = %q, want srvtoolu_1", tr.ToolCallID)
	}
	if tr.ToolName != "web_fetch" {
		t.Errorf("ToolName = %q, want web_fetch", tr.ToolName)
	}
	if !tr.ProviderExecuted {
		t.Error("ProviderExecuted = false, want true")
	}
	resultMap, ok := tr.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("Result = %#v, want map", tr.Result)
	}
	if resultMap["retrievedAt"] != "2026-01-01T00:00:00Z" {
		t.Errorf("Result[retrievedAt] = %v, want camelCased retrieved_at", resultMap["retrievedAt"])
	}

	if len(stream.citationDocuments) != 1 {
		t.Fatalf("citationDocuments = %#v, want 1 entry grown from the live web_fetch_tool_result", stream.citationDocuments)
	}
	if stream.citationDocuments[0].Title != "Fetched Report" {
		t.Errorf("citationDocuments[0].Title = %q, want Fetched Report", stream.citationDocuments[0].Title)
	}
}

// TestStream_LiveWebFetchToolResultError ports TS's web_fetch_tool_result_error
// branch: an errored deferred fetch result is surfaced as an isError tool
// result with the camelCased error code, and does not grow citationDocuments.
func TestStream_LiveWebFetchToolResultError(t *testing.T) {
	sseData := "" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"web_fetch_tool_result\",\"tool_use_id\":\"srvtoolu_1\",\"is_error\":true,\"content\":{\"type\":\"web_fetch_tool_result_error\",\"error_code\":\"invalid_url\"}}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeToolResult {
		t.Fatalf("chunk.Type = %v, want ChunkTypeToolResult", chunk.Type)
	}
	tr := chunk.ToolResult
	if tr.Error == nil || tr.Error.Error() != "invalid_url" {
		t.Errorf("Error = %v, want invalid_url", tr.Error)
	}
	resultMap, ok := tr.Result.(map[string]interface{})
	if !ok || resultMap["errorCode"] != "invalid_url" {
		t.Errorf("Result = %#v, want errorCode invalid_url", tr.Result)
	}
	if len(stream.citationDocuments) != 0 {
		t.Errorf("citationDocuments = %#v, want empty on error", stream.citationDocuments)
	}
}

// TestStream_LiveWebSearchToolResult ports TS's content_block_start
// "web_search_tool_result" case: a live web_search result emits a tool-result
// chunk followed by one source chunk per result (TS enqueues 'source' parts
// right after the 'tool-result' part, in the same handler).
func TestStream_LiveWebSearchToolResult(t *testing.T) {
	sseData := "" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"server_tool_use\",\"id\":\"srvtoolu_2\",\"name\":\"web_search\",\"input\":{}}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"web_search_tool_result\",\"tool_use_id\":\"srvtoolu_2\",\"content\":[" +
		"{\"type\":\"web_search_result\",\"url\":\"https://example.com/weather\",\"title\":\"Paris weather\",\"page_age\":\"1 day ago\",\"encrypted_content\":\"enc\"}" +
		"]}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	callChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() tool-call error: %v", err)
	}
	if callChunk.Type != provider.ChunkTypeToolCall {
		t.Fatalf("chunk.Type = %v, want ChunkTypeToolCall", callChunk.Type)
	}

	resultChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() tool-result error: %v", err)
	}
	if resultChunk.Type != provider.ChunkTypeToolResult {
		t.Fatalf("chunk.Type = %v, want ChunkTypeToolResult", resultChunk.Type)
	}
	tr := resultChunk.ToolResult
	if tr.ToolCallID != "srvtoolu_2" || tr.ToolName != "web_search" {
		t.Errorf("ToolResult = %+v", tr)
	}
	results, ok := tr.Result.([]map[string]interface{})
	if !ok || len(results) != 1 {
		t.Fatalf("Result = %#v, want 1 mapped result", tr.Result)
	}
	if results[0]["pageAge"] != "1 day ago" || results[0]["encryptedContent"] != "enc" {
		t.Errorf("mapped result = %#v, want camelCased pageAge/encryptedContent", results[0])
	}

	// Next chunk: the source part for the single search result, enqueued
	// immediately after the tool-result (TS: same handler, right after).
	sourceChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() source error: %v", err)
	}
	if sourceChunk.Type != provider.ChunkTypeSource {
		t.Fatalf("chunk.Type = %v, want ChunkTypeSource", sourceChunk.Type)
	}
	src := sourceChunk.SourceContent
	if src == nil || src.SourceType != "url" || src.URL != "https://example.com/weather" {
		t.Fatalf("SourceContent = %+v", src)
	}
	if src.Title != "Paris weather" {
		t.Errorf("Title = %q, want Paris weather", src.Title)
	}
	var meta map[string]struct {
		PageAge string `json:"pageAge"`
	}
	if err := json.Unmarshal(src.ProviderMetadata, &meta); err != nil {
		t.Fatalf("decode source providerMetadata: %v", err)
	}
	if meta["anthropic"].PageAge != "1 day ago" {
		t.Errorf("source providerMetadata.anthropic.pageAge = %q, want 1 day ago", meta["anthropic"].PageAge)
	}
}

// TestStream_LiveWebSearchToolResultError ports TS's
// web_search_tool_result_error branch (the non-array content case).
func TestStream_LiveWebSearchToolResultError(t *testing.T) {
	sseData := "" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"web_search_tool_result\",\"tool_use_id\":\"srvtoolu_2\",\"is_error\":true,\"content\":{\"type\":\"web_search_tool_result_error\",\"error_code\":\"too_many_requests\"}}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeToolResult {
		t.Fatalf("chunk.Type = %v, want ChunkTypeToolResult", chunk.Type)
	}
	tr := chunk.ToolResult
	if tr.Error == nil || tr.Error.Error() != "too_many_requests" {
		t.Errorf("Error = %v, want too_many_requests", tr.Error)
	}

	// No source chunks follow an error result.
	next, err := stream.Next()
	if err != io.EOF {
		t.Fatalf("Next() = %+v, %v; want io.EOF", next, err)
	}
}

// AR2 item 2: mcp_tool_use was missing from the message_start pre-population
// loop, so a deferred MCP tool call arriving in message_start.message.content
// (e.g. on a resumed/compacted stream) was silently dropped: no tool-call
// chunk, and s.mcpToolCalls was never populated for a paired mcp_tool_result
// to resolve against. Mirrors the live content_block_start "mcp_tool_use"
// case (TestMCPToolUseStreamingEmitsImmediately) but via message_start.
func TestStream_MessageStartPrePopulatedMCPToolUse(t *testing.T) {
	sseData := "" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"mcp_tool_use\",\"id\":\"mcp_1\",\"name\":\"lookup\",\"server_name\":\"weather\",\"input\":{\"city\":\"Paris\"}}],\"model\":\"claude-3-haiku-20240307\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":17,\"output_tokens\":1}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"mcp_tool_result\",\"tool_use_id\":\"mcp_1\",\"is_error\":false,\"content\":\"sunny\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	callChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() tool-call error: %v", err)
	}
	if callChunk.Type != provider.ChunkTypeToolCall {
		t.Fatalf("chunk.Type = %v, want ChunkTypeToolCall", callChunk.Type)
	}
	tc := callChunk.ToolCall
	if tc == nil || tc.ID != "mcp_1" || tc.ToolName != "lookup" {
		t.Fatalf("ToolCall = %+v", tc)
	}
	if !tc.ProviderExecuted || !tc.Dynamic {
		t.Errorf("ToolCall providerExecuted/dynamic = %v/%v, want true/true", tc.ProviderExecuted, tc.Dynamic)
	}
	if tc.Arguments["city"] != "Paris" {
		t.Errorf("Arguments = %v", tc.Arguments)
	}
	callMeta, _ := tc.ProviderMetadata["anthropic"].(map[string]interface{})
	if callMeta["type"] != "mcp-tool-use" || callMeta["serverName"] != "weather" {
		t.Errorf("ToolCall.ProviderMetadata = %+v", tc.ProviderMetadata)
	}

	resultChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() tool-result error: %v", err)
	}
	if resultChunk.Type != provider.ChunkTypeToolResult {
		t.Fatalf("chunk.Type = %v, want ChunkTypeToolResult", resultChunk.Type)
	}
	tr := resultChunk.ToolResult
	if tr.ToolCallID != "mcp_1" {
		t.Errorf("ToolCallID = %q, want mcp_1", tr.ToolCallID)
	}
	if tr.ToolName != "lookup" {
		t.Errorf("ToolName = %q, want lookup (resolved from the pre-populated mcp_tool_use)", tr.ToolName)
	}
	if !tr.Dynamic {
		t.Error("ToolResult.Dynamic should be true")
	}
	resultMeta, _ := tr.ProviderMetadata["anthropic"].(map[string]interface{})
	if resultMeta["type"] != "mcp-tool-use" || resultMeta["serverName"] != "weather" {
		t.Errorf("ToolResult.ProviderMetadata = %+v, want same as the pre-populated call's", tr.ProviderMetadata)
	}
	if tr.Result != "sunny" {
		t.Errorf("Result = %v, want sunny", tr.Result)
	}
}
