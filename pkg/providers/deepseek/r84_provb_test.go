package deepseek

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
)

// The TS case suites of this package evaluate the extended assertion forms.
func init() { tsfixture.EnableExtendedAsserts() }

// TS deepseek-files.ts: mediaType.split(';', 1)[0].trim().toLowerCase()
// (JavaScript trim strips a BOM).
func TestR84provbMediaTypeUsesJSTrim(t *testing.T) {
	if got := normalizeDeepSeekMediaType("\ufeffText/Plain; charset=utf-8"); got != "text/plain" {
		t.Errorf("media type = %q", got)
	}
}
