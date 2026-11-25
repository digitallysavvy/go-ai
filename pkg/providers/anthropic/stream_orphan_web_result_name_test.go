package anthropic

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS maps every web_search/web_fetch result with toCustomToolName, even when
// no server_tool_use block for the id was streamed before it.
func TestStreamWebResultWithoutServerToolUseUsesCustomToolName(t *testing.T) {
	sse := "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"web_search_tool_result\",\"tool_use_id\":\"srvtoolu_1\",\"content\":[]}}\n\n" +
		"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"web_fetch_tool_result\",\"tool_use_id\":\"srvtoolu_2\",\"content\":{\"type\":\"web_fetch_tool_result_error\",\"error_code\":\"unavailable\"}}}\n\n"
	tools := []types.Tool{
		{Name: "anthropic.web_search_20250305"},
		{Name: "anthropic.web_fetch_20250910"},
	}
	stream := newAnthropicStreamWithWarnings(io.NopCloser(strings.NewReader(sse)), false, tools, nil)
	got := map[string]string{}
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeToolResult {
			got[chunk.ToolResult.ToolCallID] = chunk.ToolResult.ToolName
		}
	}
	if got["srvtoolu_1"] != "anthropic.web_search_20250305" || got["srvtoolu_2"] != "anthropic.web_fetch_20250910" {
		t.Fatalf("tool names = %v", got)
	}
}
