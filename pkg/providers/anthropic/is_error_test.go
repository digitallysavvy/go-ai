package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS passes the mcp_tool_result block's is_error through as isError (an
// explicit false stays false, an absent flag stays unset) and keeps the
// result next to it.
func TestMCPToolResultCarriesIsErrorFlag(t *testing.T) {
	cases := []struct {
		name    string
		isError string
		want    *bool
		wantErr bool
	}{
		{"absent", ``, nil, false},
		{"false", `,"is_error":false`, boolRef(false), false},
		{"true", `,"is_error":true`, boolRef(true), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"id":"m","type":"message","role":"assistant","stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1},"content":[` +
				`{"type":"mcp_tool_use","id":"mcp_1","name":"lookup","server_name":"s","input":{}},` +
				`{"type":"mcp_tool_result","tool_use_id":"mcp_1","content":"boom"` + tc.isError + `}]}`
			var resp anthropicResponse
			if err := json.Unmarshal([]byte(body), &resp); err != nil {
				t.Fatal(err)
			}
			model := NewLanguageModel(New(Config{APIKey: "k"}), ClaudeSonnet4_6, nil)
			result := model.convertResponse(resp, false)
			var trc *types.ToolResultContent
			for i := range result.Content {
				if c, ok := result.Content[i].(types.ToolResultContent); ok {
					trc = &c
				}
			}
			if trc == nil {
				t.Fatalf("no tool result in %+v", result.Content)
			}
			if (trc.IsError == nil) != (tc.want == nil) || (tc.want != nil && *trc.IsError != *tc.want) {
				t.Errorf("IsError = %v, want %v", trc.IsError, tc.want)
			}
			if trc.Result != "boom" {
				t.Errorf("Result = %v, want the content kept next to isError", trc.Result)
			}
			if (trc.Error != "") != tc.wantErr {
				t.Errorf("Error = %q, wantErr %v", trc.Error, tc.wantErr)
			}
		})
	}
}

func boolRef(b bool) *bool { return &b }
