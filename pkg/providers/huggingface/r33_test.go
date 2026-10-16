package huggingface

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS `incomplete_details?.reason ?? 'stop'` keeps "" (mapped to "other"), and
// `if (part.tools)` is true for an empty array.
func TestR33_EmptyReasonAndEmptyToolsFollowTS(t *testing.T) {
	var resp hfResponse
	body := `{"id":"r","model":"m","created_at":1,"incomplete_details":{"reason":""},"output":[{"type":"mcp_list_tools","id":"x","server_label":"s","tools":[]}]}`
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	m := &LanguageModel{modelID: "m"}
	result := m.convertResponse(resp)
	if result.FinishReason != types.FinishReasonOther {
		t.Errorf("finish reason = %v", result.FinishReason)
	}
	var results int
	for _, c := range result.Content {
		if _, ok := c.(types.ToolResultContent); ok {
			results++
		}
	}
	if results != 1 {
		t.Errorf("tool results = %d, content = %+v", results, result.Content)
	}
}
