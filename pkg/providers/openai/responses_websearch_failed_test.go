package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"encoding/json"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// Ported from TS #22330: failed/incomplete web searches are tool errors
// carrying {status} as the result, with and without an action.
func TestResponsesWebSearchFailedStatusIsToolError_Generate(t *testing.T) {
	for _, status := range []string{"failed", "incomplete"} {
		for _, withAction := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/action=%v", status, withAction), func(t *testing.T) {
				item := map[string]interface{}{"type": "web_search_call", "id": "ws_test", "status": status}
				if withAction {
					item["action"] = map[string]interface{}{"type": "search", "query": "AI SDK"}
				}
				raw, _ := json.Marshal(item)
				p := New(Config{APIKey: "test-key"})
				model := NewResponsesLanguageModel(p, "gpt-4.1")
				tools := []types.Tool{openaitool.WebSearch(openaitool.WebSearchConfig{})}
				result, err := model.convertResponse(responses.ResponsesAPIResponse{
					ID: "resp_test", Model: "gpt-4.1",
					Output: []json.RawMessage{raw},
					Usage:  &responses.ResponsesAPIUsage{InputTokens: 1, OutputTokens: 1},
				}, true, responsesWebSearchToolName(tools), tools, nil)
				if err != nil {
					t.Fatal(err)
				}
				tr := result.Content[1].(types.ToolResultContent)
				if tr.Error != status {
					t.Errorf("Error = %q, want %q", tr.Error, status)
				}
				if res, _ := tr.Result.(map[string]interface{}); len(res) != 1 || res["status"] != status {
					t.Errorf("Result = %#v, want {status: %s}", tr.Result, status)
				}
			})
		}
	}
}

func TestResponsesWebSearchFailedStatusIsToolError_Stream(t *testing.T) {
	for _, status := range []string{"failed", "incomplete"} {
		for _, withAction := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/action=%v", status, withAction), func(t *testing.T) {
				action := ""
				if withAction {
					action = `,"action":{"type":"search","query":"AI SDK"}`
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					for _, e := range []string{
						`{"type":"response.output_item.added","output_index":0,"item":{"type":"web_search_call","id":"ws_test","status":"in_progress"}}`,
						fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":{"type":"web_search_call","id":"ws_test","status":%q%s}}`, status, action),
						`{"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":1,"output_tokens":1}}}`,
					} {
						_, _ = fmt.Fprintf(w, "data: %s\n\n", e)
					}
				}))
				defer server.Close()
				p := New(Config{APIKey: "test-key", BaseURL: server.URL})
				model := NewResponsesLanguageModel(p, "gpt-4.1")
				stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
					Prompt: types.Prompt{Messages: []types.Message{userMsg("Search")}},
					Tools:  []types.Tool{openaitool.WebSearch(openaitool.WebSearchConfig{})},
				})
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close() //nolint:errcheck
				var tr *types.ToolResult
				for {
					chunk, err := stream.Next()
					if err != nil {
						break
					}
					if chunk.Type == provider.ChunkTypeError {
						t.Fatalf("unexpected error chunk: %#v", chunk)
					}
					if chunk.Type == provider.ChunkTypeToolResult {
						tr = chunk.ToolResult
					}
				}
				if tr == nil || tr.Error == nil || tr.Error.Error() != status {
					t.Fatalf("tool result = %#v, want error %q", tr, status)
				}
				if res, _ := tr.Result.(map[string]interface{}); len(res) != 1 || res["status"] != status {
					t.Errorf("Result = %#v, want {status: %s}", tr.Result, status)
				}
			})
		}
	}
}
