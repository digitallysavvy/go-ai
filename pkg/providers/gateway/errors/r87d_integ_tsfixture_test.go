package errors

import "github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"

// Every TS plain assertion of this package's replays is evaluated, hand-ported
// or marked n/a (review round 87d); an unfiltered run fails if one stops
// evaluating (TestMain is tsfixture.PortedMain).
func init() { tsfixture.RequireEvaluated() }
