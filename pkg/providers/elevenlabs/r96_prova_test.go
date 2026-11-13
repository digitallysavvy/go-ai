package elevenlabs

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r96provaFailingWritesConn fails every write after the handshake response
// has been read.
type r96provaFailingWritesConn struct {
	net.Conn
	read atomic.Bool
}

var errR96provaWrite = errors.New("r96prova write failed")

func (c *r96provaFailingWritesConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.read.Store(true)
	return n, err
}

func (c *r96provaFailingWritesConn) Write(b []byte) (int, error) {
	if c.read.Load() {
		return 0, errR96provaWrite
	}
	return c.Conn.Write(b)
}

// TestR96provaSpeechInitSendFailureAfterStreamStart: TS onOpen enqueues
// stream-start before it sends the init message, so an init send that
// throws errors the stream after the stream-start part.
func TestR96provaSpeechInitSendFailureAfterStreamStart(t *testing.T) {
	prev := speechStreamWrapConn
	speechStreamWrapConn = func(c net.Conn) net.Conn { return &r96provaFailingWritesConn{Conn: c} }
	t.Cleanup(func() { speechStreamWrapConn = prev })

	server := newRealtimeTestServer(t)
	defer server.close()
	result, err := newSpeechTestModel(t, server.ts.URL, "eleven_flash_v2_5").DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{Text: speechTexts("Hi ")})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	parts, err := collectSpeech(t, result.Stream)
	if !errors.Is(err, errR96provaWrite) {
		t.Fatalf("err = %v, want the send error", err)
	}
	if len(parts) != 1 || parts[0].Type != provider.SpeechStreamPartTypeStreamStart {
		t.Fatalf("parts = %+v, want stream-start before the error", parts)
	}
}
