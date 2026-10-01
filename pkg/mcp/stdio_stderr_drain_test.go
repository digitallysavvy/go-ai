package mcp

import (
	"io"
	"strings"
	"testing"
)

// TestStdioTransportLogStderrDrainsAfterOverlongLine checks that logStderr
// keeps reading stderr after a line exceeds the scanner limit, so a child
// process writing more stderr never blocks on a full pipe.
func TestStdioTransportLogStderrDrainsAfterOverlongLine(t *testing.T) {
	r := strings.NewReader(strings.Repeat("x", 2*1024*1024) + "\nafter the long line\n" + strings.Repeat("y", 4096))
	transport := &StdioTransport{stderr: io.NopCloser(r)}

	done := make(chan struct{})
	go func() {
		defer close(done)
		transport.logStderr()
	}()
	<-done

	if r.Len() != 0 {
		t.Fatalf("logStderr left %d bytes of stderr unread", r.Len())
	}
}
