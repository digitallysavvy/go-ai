package openresponses

import (
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
)

// Every TS plain assertion of this package's replays is evaluated, hand-ported
// or marked n/a (review round 87d); an unfiltered run fails if one stops
// evaluating.
func init() { tsfixture.RequireEvaluated() }

func TestMain(m *testing.M) { os.Exit(tsfixture.PortedMain(m)) }
