package ai

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r91provcTranscribeIntegration records the transcribe start and end events.
type r91provcTranscribeIntegration struct {
	telemetry.NoopTelemetryIntegration
	mu    sync.Mutex
	start *telemetry.TelemetryStartEvent
	end   *telemetry.TelemetryFinishEvent
}

func (i *r91provcTranscribeIntegration) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	i.mu.Lock()
	i.start = &e
	i.mu.Unlock()
	return ctx
}

func (i *r91provcTranscribeIntegration) OnFinish(_ context.Context, e telemetry.TelemetryFinishEvent) {
	i.mu.Lock()
	i.end = &e
	i.mu.Unlock()
}

// TestR91provcTranscribeTelemetryEventFields (core1-05, transcribe sites):
// the start event carries maxRetries, providerOptions and
// timestampGranularities, and the end event segments, words, language,
// durationInSeconds, warnings and response (TS transcription-events.ts).
func TestR91provcTranscribeTelemetryEventFields(t *testing.T) {
	duration := 2.5
	model := &testutil.MockTranscriptionModel{
		ModelName: "whisper-1",
		DoTranscribeFunc: func(_ context.Context, _ *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			return &types.TranscriptionResult{
				Text:              "hello world",
				Segments:          []types.TranscriptionTimestamp{{Text: "hello world", Start: 0, End: 2.5}},
				Words:             []types.TranscriptionWord{{Text: "hello", StartSecond: 0, EndSecond: 1}},
				Language:          "en",
				DurationInSeconds: &duration,
				Warnings:          []types.Warning{{Type: "other", Message: "w"}},
				Response:          &types.ResponseMetadata{ModelID: "whisper-1", Headers: map[string]string{"X-Id": "1"}},
			}, nil
		},
	}
	integ := &r91provcTranscribeIntegration{}
	_, err := Transcribe(context.Background(), TranscribeOptions{
		Model:                  model,
		Audio:                  []byte("RIFF....WAVEfmt "),
		ProviderOptions:        map[string]interface{}{"openai": map[string]interface{}{"language": "en"}},
		TimestampGranularities: []string{"word"},
		Telemetry:              &TelemetrySettings{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{integ}},
	})
	if err != nil {
		t.Fatal(err)
	}
	integ.mu.Lock()
	defer integ.mu.Unlock()
	s := integ.start
	if s == nil || s.MaxRetries == nil || *s.MaxRetries != 2 {
		t.Fatalf("start MaxRetries = %+v", s)
	}
	if s.ProviderOptions["openai"] == nil || len(s.TimestampGranularities) != 1 || s.TimestampGranularities[0] != "word" {
		t.Errorf("start ProviderOptions %v TimestampGranularities %v", s.ProviderOptions, s.TimestampGranularities)
	}
	e := integ.end
	if e == nil {
		t.Fatal("no end event")
	}
	if len(e.Segments) != 1 || e.Segments[0].EndSecond != 2.5 || len(e.Words) != 1 || e.Words[0].Text != "hello" {
		t.Errorf("end segments %+v words %+v", e.Segments, e.Words)
	}
	if e.Language != "en" || e.DurationInSeconds == nil || *e.DurationInSeconds != 2.5 || len(e.Warnings) != 1 {
		t.Errorf("end language %q duration %v warnings %v", e.Language, e.DurationInSeconds, e.Warnings)
	}
	if e.Response == nil || e.Response.ModelID != "whisper-1" || e.Response.Headers["X-Id"] != "1" {
		t.Errorf("end response %+v", e.Response)
	}
}

// r91provcRawWSServer completes a WebSocket handshake by hand and writes
// the given unmasked server frames, so a test can send fragments.
func r91provcRawWSServer(t *testing.T, frames ...[]byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Sec-WebSocket-Key")
		sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		_, _ = buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
			base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n")
		for _, f := range frames {
			_, _ = buf.Write(f)
		}
		_ = buf.Flush()
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// TestR91provcRealtimeReceivesWholeMessages (core2-02, realtime_session
// site): a fragmented text message with a UTF-8 sequence split across
// fragments is one message, as the TS WebSocket message event delivers it.
func TestR91provcRealtimeReceivesWholeMessages(t *testing.T) {
	frame := func(fin bool, opcode byte, payload []byte) []byte {
		b0 := opcode
		if fin {
			b0 |= 0x80
		}
		return append([]byte{b0, byte(len(payload))}, payload...)
	}
	url := r91provcRawWSServer(t,
		frame(false, 0x1, []byte{'{', '"', 'a', '"', ':', '"', 0xc3}),
		frame(true, 0x0, []byte{0xa9, '"', '}'}),
	)
	rawConn, err := WebSocketRealtimeDialer{}.Dial(context.Background(), provider.WebSocketConfig{URL: url})
	if err != nil {
		t.Fatal(err)
	}
	defer rawConn.Close() //nolint:errcheck
	data, binary, err := rawConn.(RealtimeBinaryConn).ReceiveFrame(context.Background())
	if err != nil || binary || string(data) != `{"a":"é"}` {
		t.Fatalf("ReceiveFrame = %q binary=%v err=%v", data, binary, err)
	}
}
