package fx

import (
	"strings"
	"testing"
	"time"
)

// The balanced-brace scan checks the context too: braces nested 32000 deep
// (a 64 KiB pattern) make each balancedBraces call scan the whole pattern, so
// a compile that checked only between calls returns about 0.75 s (2.5 s under
// -race) after a 20 ms deadline instead of at once.
func TestR51IntegratorDeepBraceScanStopsPromptly(t *testing.T) {
	p := strings.Repeat("{", 32000) + "a,b" + strings.Repeat("}", 32000)
	compileWithin(t, p, 400*time.Millisecond)
}
