package vercel

import "testing"

// @vercel/sandbox 3.9.0 command.js STDIN_CHUNK_BYTES = 512 * 1024 (survivor
// test: TestR87toolsSpawnStdinChunks derives its expectation from the Go
// constant).
func TestR87integStdinChunkBytesMatchesTS(t *testing.T) {
	if stdinChunkBytes != 512*1024 {
		t.Fatalf("stdinChunkBytes = %d, want 524288", stdinChunkBytes)
	}
}
