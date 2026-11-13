package telemetry

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A Go system message can hold several text parts (value or pointer); TS
// SystemModelMessage content is one string, so legacy ai.prompt writes the
// parts' texts concatenated in order. Non-text parts add nothing.
func TestR51IntegratorLegacySystemMessagesJoinTextParts(t *testing.T) {
	got, err := json.Marshal(legacySystemMessages([]types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{
			types.TextContent{Text: "a"}, &types.TextContent{Text: "b"}, (*types.TextContent)(nil), types.TextContent{Text: "c"},
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if want := `[{"role":"system","content":"abc"}]`; string(got) != want {
		t.Fatalf("system = %s, want %s", got, want)
	}
}
