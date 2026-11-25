package moonshot

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
)

// TestR94integResponseTargetsTagged guards the unexported response types
// this package decodes with case-exact keys (jsonexact): an exported field
// without a json tag would match only a wire key spelled like the Go field
// name, so it would stop decoding (review round 94, cr-r94-carry-to-*).
func TestR94integResponseTargetsTagged(t *testing.T) {
	if bad := jsonexact.UntaggedFields(
		&moonshotLogprobsContent{},
		&moonshotUsageKnownFields{},
	); len(bad) > 0 {
		t.Fatalf("response fields without a json tag: %v", bad)
	}
}
