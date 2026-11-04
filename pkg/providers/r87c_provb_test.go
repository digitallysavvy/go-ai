package providers_test

import "github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"

// The shared OpenAI-compatible TS case suites evaluate the extended
// assertion forms, as each provider package's own suites do.
func init() { tsfixture.EnableExtendedAsserts() }
