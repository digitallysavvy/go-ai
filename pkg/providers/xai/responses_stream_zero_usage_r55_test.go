package xai

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS flush() reports explicit zero usage (cacheWrite 0 included, no raw usage)
// when the stream ended without a terminal event carrying usage.
func TestResponsesStreamWithoutUsageReportsTSZeroUsage(t *testing.T) {
	s := newXAIResponsesStream(io.NopCloser(strings.NewReader("")), false)
	defer s.Close() //nolint:errcheck
	c, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if c.Type != provider.ChunkTypeFinish || c.Usage == nil {
		t.Fatalf("chunk = %#v", c)
	}
	u := c.Usage
	if u.InputDetails == nil || u.InputDetails.CacheWriteTokens == nil || *u.InputDetails.CacheWriteTokens != 0 {
		t.Fatalf("cacheWrite = %#v, want explicit 0", u.InputDetails)
	}
	if u.Raw != nil {
		t.Fatalf("raw usage = %v, want none", u.Raw)
	}
}
