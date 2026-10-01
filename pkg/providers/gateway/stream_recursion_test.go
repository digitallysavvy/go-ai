package gateway

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestGatewayTextStream_NoStackGrowthOnLongChunklessRun is a regression test
// for BF5 (bug-review R6-4): gatewayTextStream.Next() used to tail-call
// convertChunk(), which itself recursed via `return s.Next()` for chunk
// types carrying nothing deliverable (a suppressed "raw" chunk when
// includeRawChunks is off, an empty "usage" chunk, or an unrecognized chunk
// type). Go does not eliminate that tail call, so a long run of such
// chunks within one external Next() call grew the goroutine stack without
// bound, eventually crashing the process with an unrecoverable `fatal
// error: stack overflow` -- not a panic, not recover()-able.
//
// This feeds 300,000 suppressed "raw" chunks (includeRawChunks left at its
// zero value, false), then one real "text-delta" chunk, in a subprocess
// with debug.SetMaxStack lowered so a regression would crash
// deterministically well within the test's 2s budget instead of needing
// gigabytes of real stack. gatewayTextStream is representative of the
// 3-return-value helper (convertChunk, which can also carry a genuine
// error, now returning (*StreamChunk, error, bool) instead of recursing)
// indirect-recursion pattern.
func TestGatewayTextStream_NoStackGrowthOnLongChunklessRun(t *testing.T) {
	if os.Getenv("GOAI_BF5_RECURSION_CHILD") == "1" {
		runGatewayTextStreamRecursionChild()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestGatewayTextStream_NoStackGrowthOnLongChunklessRun", "-test.v")
	cmd.Env = append(os.Environ(), "GOAI_BF5_RECURSION_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("BUG: child process crashed (stack overflow?) after a long run of chunkless events: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "BF5_CHILD_OK") {
		t.Fatalf("child process did not report success; output:\n%s", out)
	}
}

func runGatewayTextStreamRecursionChild() {
	debug.SetMaxStack(8 << 20) // 8 MiB, so a regression crashes quickly and deterministically.
	const n = 300_000
	var sb strings.Builder
	sb.Grow(n*48 + 256)
	for i := 0; i < n; i++ {
		// includeRawChunks is false (zero value below), so every "raw" chunk
		// is suppressed: convertChunk's "raw" case returns (nil, nil, false).
		sb.WriteString(`data: {"type":"raw","rawValue":{"provider":"chunk"}}` + "\n\n")
	}
	sb.WriteString(`data: {"type":"text-delta","id":"text_1","delta":"hello"}` + "\n\n")

	s := &gatewayTextStream{parser: streamingParser(sb.String())}
	// This call used to recurse once per suppressed raw chunk -- 300,000
	// deep -- before reaching the real text chunk.
	chunk, err := s.Next()
	if err != nil {
		fmt.Println("BF5_CHILD_FAIL: unexpected error:", err)
		return
	}
	if chunk == nil || chunk.Type != provider.ChunkTypeText || chunk.Text != "hello" {
		fmt.Printf("BF5_CHILD_FAIL: unexpected chunk: %+v\n", chunk)
		return
	}
	fmt.Println("BF5_CHILD_OK")
}
