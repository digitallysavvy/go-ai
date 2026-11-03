package deepseek

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS 4ae6986cd8 serializeDeepSeekToolCallArguments: valid JSON input text
// is sent as is, other text as its JSON string literal.
func TestR85provbDeepSeekToolCallArguments(t *testing.T) {
	for raw, want := range map[string]string{
		`{"query":"deepseek","limit":3}`: `{"query":"deepseek","limit":3}`,
		`not json`:                       `"not json"`,
		`[1]`:                            `[1]`,
	} {
		got := deepseekToolCallsWire([]types.ToolCall{{ID: "c", ToolName: "t", RawArguments: raw}})[0]["function"].(map[string]interface{})["arguments"]
		if got != want {
			t.Errorf("%q: got %v, want %s", raw, got, want)
		}
	}
}
