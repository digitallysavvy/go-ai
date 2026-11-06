package harness

import (
	"errors"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestDecodedErrorPartKeepsWireKeyOrder(t *testing.T) {
	part, err := DecodeStreamPart([]byte(`{"type":"error","error":{"z":1,"a":{"y":2.0,"b":3},"7":true,"name":"x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	ep := part.(*ErrorPart)
	want := `{"7":true,"z":1,"a":{"y":2,"b":3},"name":"x"}`
	if got := ep.Message(); got != want {
		t.Fatalf("Message = %s, want %s", got, want)
	}
	if got := ep.AsError().Error(); got != want {
		t.Fatalf("AsError = %s, want %s", got, want)
	}
	ep.Error = map[string]any{"b": 1, "a": 2}
	if got := ep.Message(); got != `{"a":2,"b":1}` {
		t.Fatalf("replaced Error should not use stale text, got %s", got)
	}
}

func TestErrorValueMessageKeepsLineSeparatorsRaw(t *testing.T) {
	if got := ErrorValueMessage(map[string]any{"a": "x y<"}); got != "{\"a\":\"x y<\"}" {
		t.Fatalf("got %q", got)
	}
}

func TestConvertToolModelOutputJSONIsNotHTMLEscaped(t *testing.T) {
	content, _, err := ConvertToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]any{"a": "<b>& "}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := content[0].Text, "{\"a\":\"<b>& \"}"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFormatBridgeErrorFallbackIsNotHTMLEscaped(t *testing.T) {
	if got := FormatBridgeError([]any{"<x>"}); got != `["<x>"]` {
		t.Fatalf("got %s", got)
	}
}

// TS getHarnessErrorMessage judges the thrown value, never its cause: a base
// harness error that wraps a reviewed error reports its own message, and a
// custom-named harness error that wraps one stays masked.
func TestGetHarnessErrorMessageJudgesOutermostHarnessError(t *testing.T) {
	cap := NewCapabilityUnsupportedError("inner capability", "h", nil)
	if got := GetHarnessErrorMessage(NewHarnessError("outer", cap)); got != "outer" {
		t.Fatalf("base wrapping capability = %q", got)
	}
	custom := &HarnessError{Name: "AI_Custom", Message: "secret", Cause: cap}
	if got := GetHarnessErrorMessage(custom); got != "An error occurred." {
		t.Fatalf("custom wrapping capability = %q", got)
	}
	if got := GetHarnessErrorMessage(fmt.Errorf("wrap: %w", cap)); got != "inner capability" {
		t.Fatalf("plain wrapper = %q", got)
	}
	if got := GetHarnessErrorMessage(errors.New("db password")); got != "An error occurred." {
		t.Fatalf("plain error = %q", got)
	}
}
