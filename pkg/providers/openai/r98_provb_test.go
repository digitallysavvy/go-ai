package openai

import (
	"context"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// r98provbDrain reads a stream to its end and returns the parts and the
// final error (nil at a clean end).
func r98provbDrain(t *testing.T, s provider.TextStream) ([]*provider.StreamChunk, error) {
	t.Helper()
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			if err == io.EOF {
				return out, nil
			}
			return out, err
		}
		out = append(out, c)
	}
}

// TestR98provbChatErrorKeyOnDataChunk: TS openaiChatChunkSchema tries the
// data member first, and its z.object strips an `error` key, so a frame with
// `choices` and an `error` member (null or an object) is a data chunk: the
// text is streamed and `'error' in value` is false. Go routed it as an error
// (a failed call before output).
func TestR98provbChatErrorKeyOnDataChunk(t *testing.T) {
	for _, errValue := range []string{`null`, `{"message":"x"}`} {
		sse := `data: {"id":"c1","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],"error":` + errValue + "}\n\n"
		s := newOpenAIStream(io.NopCloser(strings.NewReader(sse)))
		parts, err := r98provbDrain(t, s)
		if err != nil {
			t.Fatalf("error %s: err = %v, want the data chunk", errValue, err)
		}
		text, finish := "", ""
		for _, p := range parts {
			switch p.Type {
			case provider.ChunkTypeText:
				text += p.Text
			case provider.ChunkTypeFinish:
				finish = string(p.FinishReason)
			case provider.ChunkTypeError:
				t.Errorf("error %s: unexpected error part %v", errValue, p.Err)
			}
		}
		if text != "hi" || finish != "stop" {
			t.Errorf("error %s: text %q finish %q, want hi / stop", errValue, text, finish)
		}
	}
	// A frame that fails the data member stays an error (TS error member).
	s := newOpenAIStream(io.NopCloser(strings.NewReader("data: {\"error\":{\"message\":\"boom\"}}\n\n")))
	if _, err := r98provbDrain(t, s); err == nil {
		t.Errorf("error frame: no error, want the stream error")
	}
}

// TestR98provbCompletionErrorKeyOnDataChunk: the completion chunk union's
// data member is a z.object too (OpenAI and openai-compatible), so the same
// frame is a data chunk.
func TestR98provbCompletionErrorKeyOnDataChunk(t *testing.T) {
	for _, compat := range []bool{false, true} {
		sse := `data: {"id":"c1","choices":[{"text":"hi","index":0,"finish_reason":"stop"}],"error":null}` + "\n\n"
		s := newCompletionStream(io.NopCloser(strings.NewReader(sse)), false)
		s.compat = compat
		parts, err := r98provbDrain(t, s)
		if err != nil {
			t.Fatalf("compat %v: err = %v", compat, err)
		}
		text := ""
		for _, p := range parts {
			if p.Type == provider.ChunkTypeError {
				t.Errorf("compat %v: unexpected error part %v", compat, p.Err)
			}
			if p.Type == provider.ChunkTypeText {
				text += p.Text
			}
		}
		if text != "hi" {
			t.Errorf("compat %v: text %q, want hi", compat, text)
		}
	}
}

// TestR98provbServerConnectionURLBeforeIntent: TS buildServerUrl parses
// new URL(baseURL) before its intent switch, so an invalid base URL fails
// with the URL error even when the intent is unknown too.
func TestR98provbServerConnectionURLBeforeIntent(t *testing.T) {
	m := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: "not a url"}), "gpt-realtime")
	_, err := m.GetServerConnection(context.Background(), provider.RealtimeServerConnectionOptions{Intent: "translation"})
	if err == nil || strings.Contains(err.Error(), "intent") {
		t.Fatalf("err = %v, want the invalid URL error", err)
	}
}

// TestR98provbResponsesJSNumberPlatformInt: a numeric option is sent as the
// JS number; an integral value outside the platform int (32-bit targets)
// stays a float64 instead of overflowing the int conversion.
func TestR98provbResponsesJSNumberPlatformInt(t *testing.T) {
	for in, want := range map[float64]string{3e9: "3000000000", -3e9: "-3000000000", 7: "7", 2.5: "2.5", 1e16: "10000000000000000"} {
		v, ok := responsesJSNumber(in)
		b, _ := jsonstr.Marshal(v)
		if !ok || string(b) != want {
			t.Errorf("%v: %s, want %s", in, b, want)
		}
	}
}

// TestR98provbLiveMsPlatformInt: a Live start_ms / end_ms / offset_ms beyond
// the platform int is clamped instead of an implementation-defined
// conversion (a negative value on amd64, overflow on 32-bit targets).
func TestR98provbLiveMsPlatformInt(t *testing.T) {
	events := parseOpenAILiveServerEvent([]byte(`{"type":"session.output_transcript.delta","delta":"x","start_ms":1,"end_ms":1e19}`))
	if len(events) != 1 || events[0].EndMs == nil || *events[0].EndMs != math.MaxInt || *events[0].StartMs != 1 {
		t.Fatalf("events = %+v, want end clamped to MaxInt", events)
	}
	events = parseOpenAILiveServerEvent([]byte(`{"type":"session.delegation.created","delegation":{"id":"d"},"offset_ms":1e19}`))
	if len(events) != 1 || events[0].OffsetMs == nil || *events[0].OffsetMs != math.MaxInt {
		t.Fatalf("events = %+v, want offset clamped to MaxInt", events)
	}
	if got := liveMsInt(2.9); got != 2 {
		t.Errorf("liveMsInt(2.9) = %d, want 2", got)
	}
}
