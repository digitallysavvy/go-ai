package bedrock

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock/eventstream"
)

// TestBedrockConverseStream_NoStackGrowthOnLongChunklessRun is a regression
// test for BF5 (bug-review R6-4): bedrockConverseStream.Next() used to
// recurse via `return s.Next()` for event types with no handling branch
// (e.g. "messageStart", which the switch doesn't match). Go does not
// eliminate that tail call, so a long run of such events within one
// external Next() call grew the goroutine stack without bound, eventually
// crashing the process with an unrecoverable `fatal error: stack
// overflow` -- not a panic, not recover()-able.
//
// This feeds 300,000 unhandled-type AWS event-stream frames, then one real
// contentBlockDelta frame, in a subprocess with debug.SetMaxStack lowered
// so a regression would crash deterministically well within the test's 2s
// budget instead of needing gigabytes of real stack.
func TestBedrockConverseStream_NoStackGrowthOnLongChunklessRun(t *testing.T) {
	if os.Getenv("GOAI_BF5_RECURSION_CHILD") == "1" {
		runBedrockConverseStreamRecursionChild()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestBedrockConverseStream_NoStackGrowthOnLongChunklessRun", "-test.v")
	cmd.Env = append(os.Environ(), "GOAI_BF5_RECURSION_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("BUG: child process crashed (stack overflow?) after a long run of chunkless events: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "BF5_CHILD_OK") {
		t.Fatalf("child process did not report success; output:\n%s", out)
	}
}

func runBedrockConverseStreamRecursionChild() {
	debug.SetMaxStack(8 << 20) // 8 MiB, so a regression crashes quickly and deterministically.
	const n = 300_000
	events := make([][2]string, 0, n+1)
	for i := 0; i < n; i++ {
		// "messageStart" matches no case in Next()'s payloadType switch:
		// falls through to the trailing `continue`.
		events = append(events, [2]string{"messageStart", `{}`})
	}
	events = append(events, [2]string{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"text":"hello"}}`})
	body := buildBedrockEventStreamBody(events)

	s := &bedrockConverseStream{
		decoder:       eventstream.NewDecoder(bytes.NewReader(body)),
		body:          io.NopCloser(bytes.NewReader(nil)),
		contentBlocks: map[int]*bedrockStreamContentBlock{},
		finishReason:  types.FinishReasonOther,
	}
	// startEmitted defaults to false, so the first Next() call returns the
	// synthesized stream-start/response-metadata chunks before touching the
	// decoder at all; drain those first.
	for i := 0; i < 2; i++ {
		if _, err := s.Next(); err != nil {
			fmt.Println("BF5_CHILD_FAIL: unexpected error during startup chunks:", err)
			return
		}
	}
	// This call used to recurse once per unhandled event -- 300,000 deep --
	// before reaching the real text-start chunk (a text-delta opens with a
	// text-start boundary chunk; the text itself follows on the next call).
	startChunk, err := s.Next()
	if err != nil {
		fmt.Println("BF5_CHILD_FAIL: unexpected error on text-start:", err)
		return
	}
	if startChunk == nil || startChunk.Type != provider.ChunkTypeTextStart {
		fmt.Printf("BF5_CHILD_FAIL: unexpected chunk: %+v\n", startChunk)
		return
	}
	chunk, err := s.Next()
	if err != nil {
		fmt.Println("BF5_CHILD_FAIL: unexpected error on text chunk:", err)
		return
	}
	if chunk == nil || chunk.Type != provider.ChunkTypeText || chunk.Text != "hello" {
		fmt.Printf("BF5_CHILD_FAIL: unexpected chunk: %+v\n", chunk)
		return
	}
	fmt.Println("BF5_CHILD_OK")
}
