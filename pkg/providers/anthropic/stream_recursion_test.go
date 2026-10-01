package anthropic

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
// (bug-review R6-4): anthropicStream.Next() used to recurse via `return
// s.Next()` whenever an SSE event produced no chunk -- e.g. a "ping"
// keep-alive, which Anthropic sends periodically during long streams. Go
// does not eliminate that tail call, so a long run of such events within
// one external Next() call grew the goroutine stack without bound,
// eventually crashing the process with an unrecoverable `fatal error:
// stack overflow` -- not a panic, not recover()-able.
//
// This feeds 300,000 "ping" keep-alive events, then one real
// content_block_start event for a text block, in a subprocess with
// debug.SetMaxStack lowered so a regression would crash deterministically
// well within the test's 2s budget instead of needing gigabytes of real
// stack. anthropicStream is representative of the plain self-recursive
// (no helper indirection) Next() pattern also used by mistralStream,
// cohereV2Stream, bedrockConverseStream, moonshotStream,
// openAIStream/completionStream, and interactionsStream.
func TestStream_NoStackGrowthOnLongChunklessRun(t *testing.T) {
	if os.Getenv("GOAI_BF5_RECURSION_CHILD") == "1" {
		runAnthropicStreamRecursionChild()
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

func runAnthropicStreamRecursionChild() {
	debug.SetMaxStack(8 << 20) // 8 MiB, so a regression crashes quickly and deterministically.
	const n = 300_000
	var sb strings.Builder
	sb.Grow(n*24 + 256)
	for i := 0; i < n; i++ {
		sb.WriteString("event: ping\ndata: {\"type\":\"ping\"}\n\n")
	}
	sb.WriteString(`event: content_block_start` + "\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n")

	s := newAnthropicStream(io.NopCloser(strings.NewReader(sb.String())), false)
	// This call used to recurse once per "ping" keep-alive event -- 300,000
	// deep -- before reaching the real text-start chunk.
	chunk, err := s.Next()
	if err != nil {
		fmt.Println("BF5_CHILD_FAIL: unexpected error:", err)
		return
	}
	if chunk == nil || chunk.Type != provider.ChunkTypeTextStart {
		fmt.Printf("BF5_CHILD_FAIL: unexpected chunk: %+v\n", chunk)
		return
	}
	fmt.Println("BF5_CHILD_OK")
}
