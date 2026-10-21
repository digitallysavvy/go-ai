package ai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// A reasoning-file chunk without content is dropped, like the file and custom
// chunks, instead of panicking the conversion goroutine.
func TestToUIMessageChunk_ReasoningFileWithoutContent(t *testing.T) {
	if _, ok := ToUIMessageChunk(provider.StreamChunk{Type: provider.ChunkTypeReasoningFile}, UIMessageStreamResultOptions{}); ok {
		t.Fatal("expected no UI chunk for a reasoning-file chunk without content")
	}
}
