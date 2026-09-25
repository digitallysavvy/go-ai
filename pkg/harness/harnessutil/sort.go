package harnessutil

import "github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"

// sortStrings sorts with JavaScript localeCompare semantics.
func sortStrings(values []string) { posixpath.SortStrings(values) }
