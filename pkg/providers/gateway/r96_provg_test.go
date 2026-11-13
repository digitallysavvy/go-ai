package gateway

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR96provgV4ToolErrorTextIsJSONStringify: an isError tool result whose
// value is not a string is JSON.stringify of the value (TS), so HTML
// characters stay literal (cr-r96-stream-to-provg 1).
func TestR96provgV4ToolErrorTextIsJSONStringify(t *testing.T) {
	var p v4Part
	if err := json.Unmarshal([]byte(`{"type":"tool-result","toolCallId":"c","toolName":"t","isError":true,"result":{"message":"a<b&c>"}}`), &p); err != nil {
		t.Fatal(err)
	}
	const want = `{"message":"a<b&c>"}`
	if tr := p.toolResult(); tr.Error == nil || tr.Error.Error() != want {
		t.Errorf("stream tool error = %v, want %s", tr.Error, want)
	}
	part, _ := decodeV4Content(p)
	if tr, ok := part.(types.ToolResultContent); !ok || tr.Error != want {
		t.Errorf("generate tool error = %#v, want %s", part, want)
	}
}
