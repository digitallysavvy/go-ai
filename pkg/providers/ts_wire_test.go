package providers_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
)

// TestTSWire replays every recorded TS provider test (testdata/tswire, see
// pkg/internal/tsfixture/record/README.md) through the Go provider of the
// same name: the HTTP requests the Go model sends are diffed against the
// requests the TS model sent, and the returned values against the TS result.
//
// TSWIRE_PKG=<ts package> narrows the run; TSWIRE_REPORT=1 prints all
// differences instead of failing on them (used when triaging).
func TestTSWire(t *testing.T) {
	files, _ := filepath.Glob("testdata/tswire/*.jsonl.gz")
	if len(files) == 0 {
		t.Skip("no recordings")
	}
	only := os.Getenv("TSWIRE_PKG")
	for _, f := range files {
		pkg, _, _ := strings.Cut(strings.TrimSuffix(filepath.Base(f), ".jsonl.gz"), "__")
		if only != "" && only != pkg {
			continue
		}
		suite, ok := wireSuites[pkg]
		if !ok {
			continue
		}
		f, suite := f, suite
		t.Run(pkg, func(t *testing.T) { tsfixture.RunWire(t, f, suite) })
	}
}
