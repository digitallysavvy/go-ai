package google

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

var errR96provgWrite = errors.New("r96provg: write failed")

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
		return 0, errR96provgWrite
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
	if part, err := result.Stream.Next(); !errors.Is(err, errR96provgWrite) {
		t.Fatalf("second = %+v, %v; want the write error", part, err)
	}
}

func TestR96provgSpeechTranslationSetupSendFailureAfterStreamStart(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()
	r96provgFailSetupWrites(t)
	result, err := newSpeechTranslationTestModel(t, server.ts.URL, 0).DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio: newChanAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"}, TargetLanguage: "es",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	part, err := result.Stream.Next()
	if err != nil || part.Type != provider.SpeechTranslationStreamPartTypeStreamStart {
		t.Fatalf("first part = %+v, %v; want stream-start", part, err)
	}
	if part, err := result.Stream.Next(); !errors.Is(err, errR96provgWrite) {
		t.Fatalf("second = %+v, %v; want the write error", part, err)
	}
}

// TestR96provgInteractionsFramePrototypeKeyIsJSONParseError: TS
// secureJsonParse fails such a frame (never the loose unknownEvent
// dispatch), a JSONParseError error part.
func TestR96provgInteractionsFramePrototypeKeyIsJSONParseError(t *testing.T) {
	body := `data: {"event_type":"step.start","index":0,"step":{"type":"thought"},"__proto__":{}}` + "\n\n" +
		`data: {"event_type":"interaction.completed","interaction":{"id":"v1","status":"completed"}}` + "\n\n"
	s := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	var sawParseErr bool
	for _, c := range drainInteractions(t, s) {
		if c.Type == provider.ChunkTypeReasoningStart {
			t.Fatal("the frame was dispatched")
		}
		if c.Type == provider.ChunkTypeError {
			var pe *providererrors.JSONParseError
			if !errors.As(c.Err, &pe) {
				t.Fatalf("error part = %T %v, want *JSONParseError", c.Err, c.Err)
			}
			sawParseErr = true
		}
	}
	if !sawParseErr {
		t.Fatal("no error part")
	}
}
