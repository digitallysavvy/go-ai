package middleware

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestDefaultFormatExampleDoesNotEscapeHTML(t *testing.T) {
	got := defaultFormatExample(types.ToolInputExample{Input: map[string]interface{}{"q": "a<b>&c"}}, 0)
	if want := `{"q":"a<b>&c"}`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
