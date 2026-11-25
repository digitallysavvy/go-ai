package testutil

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// core3-20: Read honors the io.Reader contract, so io.ReadAll reads a chunk
// longer than its buffer and skips non-text chunks.
func TestR91integMockTextStreamRead(t *testing.T) {
	long := strings.Repeat("x", 2000)
	s := NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: long},
		{Type: provider.ChunkTypeFinish},
		{Type: provider.ChunkTypeText, Text: "!"},
	})
	b, err := io.ReadAll(s)
	if err != nil || string(b) != long+"!" {
		t.Fatalf("ReadAll = %d bytes, %v", len(b), err)
	}
}
