package vercel

import (
	"bufio"
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRunCommandWait_OversizedLine_PreservesCauseAndPartialOutput is the
// regression test for bug-review/R3-3: RunCommandWait's ndjson scanner caps
// a single line at vercelNDJSONMaxLine (16MB). A command whose stdout is
// emitted as one oversized line (common for base64/long-line output) makes
// bufio.Scanner fail with bufio.ErrTooLong. That real cause used to be
// captured (as scanner.Err()) and passed to streamEndedEarly, but
// streamEndedEarly's cause parameter was never used — it unconditionally
// returned the generic "Stream ended before command data was received",
// making a client-side 16MB/line limit indistinguishable from a genuine
// sandbox-reported stream failure. The function also returned a nil result,
// discarding any stdout/stderr already accumulated before the oversized
// line.
//
// This serves one small stdout line, then one line whose JSON encoding
// exceeds 16MB, and asserts the returned error (a) mentions/wraps the real
// cause (bufio.ErrTooLong) instead of only the generic message, and (b) the
// returned result is non-nil and still carries the stdout accumulated before
// the failure.
func TestRunCommandWait_OversizedLine_PreservesCauseAndPartialOutput(t *testing.T) {
	const sessionID = "sess_test"
	oversized := strings.Repeat("A", vercelNDJSONMaxLine+1024)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		var buf bytes.Buffer
		writeNDJSON(&buf, commandResponse{Command: commandWire{ID: "cmd1", SessionID: sessionID}})
		writeNDJSON(&buf, ndjsonLine{Stream: "stdout", Data: "hello "})
		writeNDJSON(&buf, ndjsonLine{Stream: "stdout", Data: oversized})
		_, _ = w.Write(buf.Bytes())
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	client := NewAPIClient(srv.URL, Credentials{Token: "tok"})
	result, err := client.RunCommandWait(t.Context(), sessionID, runCommandRequest{Command: "echo", Args: []string{"hi"}})

	if err == nil {
		t.Fatal("RunCommandWait err = nil, want an oversized-line failure")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("errors.Is(err, bufio.ErrTooLong) = false; err = %v", err)
	}
	if !strings.Contains(err.Error(), "ndjson") && !strings.Contains(err.Error(), "too long") {
		t.Fatalf("err.Error() = %q, want it to mention the real cause (an oversized ndjson line), not just the generic stream-ended message", err.Error())
	}

	var streamErr *StreamError
	if !errors.As(err, &streamErr) {
		t.Fatalf("errors.As(err, *StreamError) = false; err = %v (%T)", err, err)
	}
	if streamErr.Code != "stream_ended_early" {
		t.Fatalf("StreamError.Code = %q, want stream_ended_early", streamErr.Code)
	}

	if result == nil {
		t.Fatal("result = nil, want the stdout accumulated before the failure")
	}
	if result.Stdout != "hello " {
		t.Fatalf("result.Stdout = %q, want %q (accumulated before the oversized line)", result.Stdout, "hello ")
	}
}

// TestRunCommandWait_StreamEndedEarly_WrapsRealCause is a narrower unit test
// for the same R3-3 fix at the streamEndedEarly/wrapScanError level: a
// genuine client-side read error (not just bufio.ErrTooLong) must also
// survive into the returned error instead of being replaced by the generic
// message.
func TestRunCommandWait_StreamEndedEarly_WrapsRealCause(t *testing.T) {
	cause := errors.New("connection reset by peer")
	err := streamEndedEarly("sess_test", cause)
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(err, cause) = false; err = %v", err)
	}
	if !strings.Contains(err.Error(), cause.Error()) {
		t.Fatalf("err.Error() = %q, want it to contain %q", err.Error(), cause.Error())
	}

	// A nil cause (a clean EOF with no scan error) must still produce the
	// original generic message, unchanged.
	plain := streamEndedEarly("sess_test", nil)
	if !strings.Contains(plain.Error(), "Stream ended before command data was received") {
		t.Fatalf("streamEndedEarly(nil) = %q, want the generic message preserved", plain.Error())
	}
}
