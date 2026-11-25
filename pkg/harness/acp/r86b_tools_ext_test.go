package acp_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
	"github.com/digitallysavvy/go-ai/pkg/harness/githubcopilot"
)

// V061 item 26: TS resolveGitHubCopilotHostToolCall opts out on
// `toolCall.name != null`, so a reported `name: ""` is a named call (no
// resolution) while an absent or null name resolves by title. Recorded with
// node running the TS resolver on JSON.parse of each tool call
// (testdata/r86b_tools_resolver_name_ts.json); Go decodes the same tool call
// from an acp-host-tool-call-resolution-request bridge frame.
func TestR86btoolsCopilotResolverNamePresenceMatchesTS(t *testing.T) {
	data, err := os.ReadFile("testdata/r86b_tools_resolver_name_ts.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			ToolCall string `json:"toolCall"`
			TS       *struct {
				ToolName string         `json:"toolName"`
				Input    map[string]any `json:"input"`
			} `json:"ts"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range fixture.Cases {
		frame := `{"type":"acp-host-tool-call-resolution-request","requestId":"r","serverName":"host","toolNames":["search"],"toolCall":` + c.ToolCall + `}`
		msg, _, err := acp.R86btoolsDecodeOutbound([]byte(frame))
		if err != nil {
			t.Fatalf("%s: %v", c.ToolCall, err)
		}
		f := msg.(acp.HostToolCallResolutionRequestFrame)
		got := githubcopilot.ResolveHostToolCall(acp.ResolveHostToolCallOptions{ToolCall: f.ToolCall, ServerName: f.ServerName, ToolNames: f.ToolNames})
		switch {
		case c.TS == nil && got != nil:
			t.Errorf("%s: resolved %+v, want none", c.ToolCall, got)
		case c.TS != nil && (got == nil || got.ToolName != c.TS.ToolName || !reflect.DeepEqual(got.Input, c.TS.Input)):
			t.Errorf("%s: resolved %+v, want %+v", c.ToolCall, got, c.TS)
		}
	}
}
