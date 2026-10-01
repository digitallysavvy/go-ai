package alibaba

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestStream_NoStackGrowthOnLongChunklessRun is a regression test for BF5
// (bug-review R6-4): alibabaStream.Next() used to tail-call processChunk(),
// which itself recursed via `return s.Next()` for choices-less chunks (and
// also tail-called emitParsedChunk(), which recursed via `return
// s.Next()` too). Go does not eliminate either tail call, so a long run of
// such events within one external Next() call grew the goroutine stack
// without bound, eventually crashing the process with an unrecoverable
// `fatal error: stack overflow` -- not a panic, not recover()-able.
//
// This feeds 300,000 choices-less events, then one real text delta, in a
// subprocess with debug.SetMaxStack lowered so a regression would crash
// deterministically well within the test's 2s budget instead of needing
// gigabytes of real stack.
func TestStream_NoStackGrowthOnLongChunklessRun(t *testing.T) {
	if os.Getenv("GOAI_BF5_RECURSION_CHILD") == "1" {
		runAlibabaStreamRecursionChild()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestStream_NoStackGrowthOnLongChunklessRun", "-test.v")
	cmd.Env = append(os.Environ(), "GOAI_BF5_RECURSION_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("BUG: child process crashed (stack overflow?) after a long run of chunkless SSE events: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "BF5_CHILD_OK") {
		t.Fatalf("child process did not report success; output:\n%s", out)
	}
}

func runAlibabaStreamRecursionChild() {
	debug.SetMaxStack(8 << 20) // 8 MiB, so a regression crashes quickly and deterministically.
	const n = 300_000
	var sb strings.Builder
	sb.Grow(n*16 + 128)
	for i := 0; i < n; i++ {
		// No "choices" field: processChunk's `if len(chunk.Choices) == 0`
		// branch.
		sb.WriteString(`data: {}` + "\n\n")
	}
	sb.WriteString(`data: {"choices":[{"delta":{"content":"hello"},"finish_reason":null}]}` + "\n\n")

	s := newAlibabaStream(io.NopCloser(strings.NewReader(sb.String())))
	// This call used to recurse once per choices-less event -- 300,000 deep
	// -- before reaching the real text chunk.
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
