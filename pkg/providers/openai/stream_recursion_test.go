package openai

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
// for BF5 (bug-review R6-4): responsesStream.Next() used to recurse via
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
// responsesStream is representative of the double-helper
// (emitParsedChunk + handleOutputItemDone, both now returning
// (*StreamChunk, bool) instead of recursing) indirect-recursion pattern
// also used by xaiResponsesStream.
func TestResponsesStream_NoStackGrowthOnLongChunklessRun(t *testing.T) {
	if os.Getenv("GOAI_BF5_RECURSION_CHILD") == "1" {
		runResponsesStreamRecursionChild()
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

func runResponsesStreamRecursionChild() {
	debug.SetMaxStack(8 << 20) // 8 MiB, so a regression crashes quickly and deterministically.
	const n = 300_000
	var sb strings.Builder
	sb.Grow(n*32 + 256)
	for i := 0; i < n; i++ {
		// Unrecognized event type: hits Next()'s `default: continue` branch.
		sb.WriteString(`data: {"type":"response.unrecognized_event"}` + "\n\n")
	}
	sb.WriteString(`data: {"type":"response.output_text.delta","output_index":0,"delta":"hello"}` + "\n\n")

	s := newResponsesStream(io.NopCloser(strings.NewReader(sb.String())), false)
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

// TestOpenAIStream_NoStackGrowthOnLongChunklessRun is a regression test for
// BF5 (bug-review R6-4): openAIStream.Next() (the Chat Completions stream)
// used to recurse via `return s.Next()` on every empty/unrecognised chunk
// (no content, no tool calls, no finish_reason). Go does not eliminate
// that tail call, so a long run of such chunks within one external Next()
// call grew the goroutine stack without bound, eventually crashing the
// process with an unrecoverable `fatal error: stack overflow` -- not a
// panic, not recover()-able.
//
// This feeds 300,000 empty chunks, then one real text delta, in a
// subprocess with debug.SetMaxStack lowered so a regression would crash
// deterministically well within the test's 2s budget instead of needing
// gigabytes of real stack.
func TestOpenAIStream_NoStackGrowthOnLongChunklessRun(t *testing.T) {
	if os.Getenv("GOAI_BF5_RECURSION_CHILD") == "1" {
		runOpenAIStreamRecursionChild()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestOpenAIStream_NoStackGrowthOnLongChunklessRun", "-test.v")
	cmd.Env = append(os.Environ(), "GOAI_BF5_RECURSION_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("BUG: child process crashed (stack overflow?) after a long run of chunkless SSE events: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "BF5_CHILD_OK") {
		t.Fatalf("child process did not report success; output:\n%s", out)
	}
}

func runOpenAIStreamRecursionChild() {
	debug.SetMaxStack(8 << 20) // 8 MiB, so a regression crashes quickly and deterministically.
	const n = 300_000
	var sb strings.Builder
	sb.Grow(n*16 + 128)
	for i := 0; i < n; i++ {
		// No "choices" field: falls through every branch to the final
		// `return s.Next()` (now `continue`).
		sb.WriteString(`data: {}` + "\n\n")
	}
	sb.WriteString(`data: {"choices":[{"delta":{"content":"hello"},"finish_reason":null}]}` + "\n\n")
	sb.WriteString("data: [DONE]\n\n")

	s := newOpenAIStream(io.NopCloser(strings.NewReader(sb.String())))
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

// TestCompletionStream_NoStackGrowthOnLongChunklessRun is a regression test
// for BF5 (bug-review R6-4): completionStream.Next() (the legacy
// Completions stream) used to recurse via `return s.Next()` whenever a
// chunk carried no text (e.g. a chunk with an empty choices array). Go
// does not eliminate that tail call, so a long run of such chunks within
// one external Next() call grew the goroutine stack without bound,
// eventually crashing the process with an unrecoverable `fatal error:
// stack overflow` -- not a panic, not recover()-able.
//
// This feeds 300,000 choices-less chunks, then one real text delta, in a
// subprocess with debug.SetMaxStack lowered so a regression would crash
// deterministically well within the test's 2s budget instead of needing
// gigabytes of real stack.
func TestCompletionStream_NoStackGrowthOnLongChunklessRun(t *testing.T) {
	if os.Getenv("GOAI_BF5_RECURSION_CHILD") == "1" {
		runCompletionStreamRecursionChild()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestCompletionStream_NoStackGrowthOnLongChunklessRun", "-test.v")
	cmd.Env = append(os.Environ(), "GOAI_BF5_RECURSION_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("BUG: child process crashed (stack overflow?) after a long run of chunkless SSE events: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "BF5_CHILD_OK") {
		t.Fatalf("child process did not report success; output:\n%s", out)
	}
}

func runCompletionStreamRecursionChild() {
	debug.SetMaxStack(8 << 20) // 8 MiB, so a regression crashes quickly and deterministically.
	const n = 300_000
	var sb strings.Builder
	sb.Grow(n*16 + 128)
	for i := 0; i < n; i++ {
		// No "choices" field: the main body falls through to the trailing
		// `return s.Next()` (now `continue`).
		sb.WriteString(`data: {}` + "\n\n")
	}
	sb.WriteString(`data: {"choices":[{"text":"hello"}]}` + "\n\n")
	sb.WriteString("data: [DONE]\n\n")

	s := newCompletionStream(io.NopCloser(strings.NewReader(sb.String())), false)
	// The very first chunk always queues a response-metadata chunk
	// (!s.metadataEmitted), independent of its content; drain that first.
	if _, err := s.Next(); err != nil {
		fmt.Println("BF5_CHILD_FAIL: unexpected error on response-metadata:", err)
		return
	}
	// The first real text chunk opens with a text-start boundary chunk; the
	// text itself follows on the next call. This call used to recurse once
	// per choices-less event -- 300,000 deep -- before reaching it.
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
