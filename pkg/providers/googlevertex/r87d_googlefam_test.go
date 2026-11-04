package googlevertex

import (
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
)

// Every TS plain assertion of the Vertex case replay evaluates (round 87d);
// an unfiltered run fails if one stops evaluating.
func init() { tsfixture.RequireEvaluated() }

func TestMain(m *testing.M) { os.Exit(tsfixture.PortedMain(m)) }
