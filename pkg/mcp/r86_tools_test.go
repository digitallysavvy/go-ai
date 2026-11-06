package mcp

import (
	"context"
	"strings"
	"testing"
)

// TS InputRequiredResultSchema: requestState is z.string().optional() and an
// input request's params z.optional(BaseParamsSchema), so an explicit null
// in either fails the parse ("Failed to process server response") instead
// of retrying with an empty requestState or reaching the method check.
func TestR86toolsInputRequiredNullFields(t *testing.T) {
	for _, result := range []string{
		`{"resultType":"input_required","requestState":null}`,
		`{"resultType":"input_required","inputRequests":{"s":{"method":"sampling/createMessage","params":null}}}`,
	} {
		c, tr := r85toolsSetup(t, result)
		_, err := c.CallTool(context.Background(), "deploy", nil)
		if err == nil || !strings.Contains(err.Error(), "Failed to process server response") {
			t.Fatalf("%s: err = %v", result, err)
		}
		if n := len(tr.recorded()); n != 1 {
			t.Fatalf("%s: requests = %d, want 1", result, n)
		}
	}
}
