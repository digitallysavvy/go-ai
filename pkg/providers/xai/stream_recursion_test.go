package xai

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

// TestResponsesStream_NoStackGrowthOnLongChunklessRun is a regression test
// for BF5 (bug-review R6-4): xaiResponsesStream.Next() used to recurse via
// `return s.Next()` on unrecognized/no-op Responses API SSE event types,
// and two helpers it tail-calls into -- emitParsedChunk and
// handleOutputItemDone -- also recursed via `return s.Next()` internally.
// Go does not eliminate any of those tail calls, so a long run of
// unrecognized events within one external Next() call grew the goroutine
// stack without bound, eventually crashing the process with an
// unrecoverable `fatal error: stack overflow` -- not a panic, not
// recover()-able.
//
// This feeds 300,000 unrecognized-type events, then one real
// response.output_text.delta event, in a subprocess with debug.SetMaxStack
// lowered so a regression would crash deterministically well within the
// test's 2s budget instead of needing gigabytes of real stack.
func TestResponsesStream_NoStackGrowthOnLongChunklessRun(t *testing.T) {
	if os.Getenv("GOAI_BF5_RECURSION_CHILD") == "1" {
		runXAIResponsesStreamRecursionChild()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestResponsesStream_NoStackGrowthOnLongChunklessRun", "-test.v")
	cmd.Env = append(os.Environ(), "GOAI_BF5_RECURSION_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("BUG: child process crashed (stack overflow?) after a long run of chunkless SSE events: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "BF5_CHILD_OK") {
		t.Fatalf("child process did not report success; output:\n%s", out)
	}
}

func runXAIResponsesStreamRecursionChild() {
	debug.SetMaxStack(8 << 20) // 8 MiB, so a regression crashes quickly and deterministically.
	const n = 300_000
	var sb strings.Builder
	sb.Grow(n*32 + 256)
	for i := 0; i < n; i++ {
		// Unrecognized event type: hits Next()'s `default: continue` branch.
		sb.WriteString(`data: {"type":"response.unrecognized_event"}` + "\n\n")
	}
	sb.WriteString(`data: {"type":"response.output_text.delta","output_index":0,"delta":"hello"}` + "\n\n")

	s := newXAIResponsesStream(io.NopCloser(strings.NewReader(sb.String())))
	// This call used to recurse once per unrecognized event -- 300,000 deep
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
