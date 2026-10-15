package providerutils

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// JSON.stringify leaves <, > and & alone; the text must not be HTML-escaped.
func TestJSONResponseToolTextDoesNotEscapeHTML(t *testing.T) {
	got := JSONResponseToolText(types.ToolCall{Arguments: map[string]interface{}{"a": "<b>&</b>"}})
	if want := `{"a":"<b>&</b>"}`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
