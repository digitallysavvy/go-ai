package googlevertex

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

var r96provgWriteErr = errors.New("r96provg: write failed")

// r96provgFailWriteConn fails every Write once a Read has returned (the
// handshake response was read), so the first frame after the handshake (the
// setup message) fails to send.
type r96provgFailWriteConn struct {
	net.Conn
	read atomic.Bool
}

func (c *r96provgFailWriteConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.read.Store(true)
	}
	return n, err
}

func (c *r96provgFailWriteConn) Write(p []byte) (int, error) {
	if c.read.Load() {
		return 0, r96provgWriteErr
	}
	return c.Conn.Write(p)
}

func r96provgFailSetupWrites(t *testing.T) {
	testWrapConn = func(c net.Conn) net.Conn { return &r96provgFailWriteConn{Conn: c} }
	t.Cleanup(func() { testWrapConn = nil })
}

// TestR96provgLiveTranscriptionSetupSendFailureAfterStreamStart: TS onOpen
// enqueues stream-start before it sends the setup message, so a failed send
// errors the stream after that part (cr-r96-prova-to-provg).
func TestR96provgLiveTranscriptionSetupSendFailureAfterStreamStart(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()
	r96provgFailSetupWrites(t)
	result, err := newLiveTestModel(t, server.ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio: newChanAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	part, err := result.Stream.Next()
	if err != nil || part.Type != provider.TranscriptionStreamPartTypeStreamStart {
		t.Fatalf("first part = %+v, %v; want stream-start", part, err)
	}
	if part, err := result.Stream.Next(); !errors.Is(err, r96provgWriteErr) {
		t.Fatalf("second = %+v, %v; want the write error", part, err)
	}
}

