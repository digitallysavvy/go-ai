package openresponses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR86provbToolCallArgumentsMatchNode: replayed tool-call argument text
// against TS 30548ee7f9 outputs computed under Node from a literal copy of
// the TS converter's serializer (openResponses; the TS input is the parsed JSON, or
// the raw string for an invalid call).
func TestR86provbToolCallArgumentsMatchNode(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"a":1}`, `{"a":1}`},
		{`[1,2]`, `[1,2]`},
		{`"{\"a\":1}"`, `{"a":1}`},
		{`"[1]"`, `[1]`},
		{`"abc"`, `abc`},
		{`abc`, `abc`},
		{`{"a":`, `{"a":`},
		{`123`, `123`},
		{`"\"x\""`, `"x"`},
		{`{"b":"<&>"}`, `{"b":"<&>"}`},
	} {
		if got := serializeToolCallArguments(types.ToolCallContent{ToolCallID: "c", ToolName: "f", Input: tc.raw}); got != tc.want {
			t.Errorf("%s: arguments = %s, want %s", tc.raw, got, tc.want)
		}
	}
}
