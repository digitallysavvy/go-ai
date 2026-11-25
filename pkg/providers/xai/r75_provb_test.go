package xai

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR75ProvbTSShapedToolArgsSentAsGiven: TS prepareResponsesTools copies
// each validated arg to its wire key, so a TS-shaped tool's maxNumResults 0
// or empty serverLabel reaches the wire (Go dropped zero values), and a
// fractional maxNumResults is not truncated. A hand-built xai.mcp tool with
// an MCPServerConfig (not MCPServerOptions) sends its config (Go sent an
// empty server_url).
func TestR75ProvbTSShapedToolArgsSentAsGiven(t *testing.T) {
	wire := func(tool types.Tool) string {
		t.Helper()
		if err := validateXAIResponsesToolArgs([]types.Tool{tool}); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(prepareXAIResponsesTools([]types.Tool{tool})[0])
		return string(b)
	}
	fs := types.Tool{Name: "fs", Type: "provider", ProviderID: "xai.file_search", ProviderArgs: map[string]interface{}{"vectorStoreIds": []interface{}{"vs"}, "maxNumResults": 0.0}}
	if got := wire(fs); got != `{"max_num_results":0,"type":"file_search","vector_store_ids":["vs"]}` {
		t.Errorf("file search = %s", got)
	}
	fs.ProviderArgs["maxNumResults"] = 2.5
	if got := wire(fs); got != `{"max_num_results":2.5,"type":"file_search","vector_store_ids":["vs"]}` {
		t.Errorf("file search fractional = %s", got)
	}
	mcp := types.Tool{Name: "m", Type: "provider", ProviderID: "xai.mcp", ProviderArgs: map[string]interface{}{"serverUrl": "https://x", "serverLabel": "", "headers": map[string]interface{}{}}}
	if got := wire(mcp); got != `{"headers":{},"server_label":"","server_url":"https://x","type":"mcp"}` {
		t.Errorf("mcp = %s", got)
	}
	hand := types.Tool{Name: "xai.mcp", ProviderOptions: MCPServerConfig{ServerURL: "https://y", ServerLabel: "l"}}
	if got := wire(hand); got != `{"server_label":"l","server_url":"https://y","type":"mcp"}` {
		t.Errorf("hand-built MCPServerConfig = %s", got)
	}
	// The Go constructors keep building from the typed config.
	if got := wire(FileSearch(FileSearchConfig{VectorStoreIDs: []string{"a"}})); got != `{"type":"file_search","vector_store_ids":["a"]}` {
		t.Errorf("constructor file search = %s", got)
	}
}
