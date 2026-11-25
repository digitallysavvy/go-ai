package deepgram

import (
	"bufio"
	"context"
	"crypto/sha1" //nolint:gosec // WebSocket accept key
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r88provcFrame is one client frame the raw test server read.
type r88provcFrame struct {
	opcode byte
	data   []byte
}

// r88provcWS is a raw WebSocket server: it records the handshake, echoes
// the first offered subprotocol, collects client frames and lets the test
// write server frames verbatim.
type r88provcWS struct {
	ts *httptest.Server

	mu        sync.Mutex
	requests  []*http.Request
	protocols []string
	frames    []r88provcFrame
	conn      net.Conn
	connected chan struct{}
}

func r88provcNewWS(t *testing.T) *r88provcWS {
	t.Helper()
	s := &r88provcWS{connected: make(chan struct{})}
	var once sync.Once
	s.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protocols := []string{}
		for _, p := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
			if p = strings.TrimSpace(p); p != "" {
				protocols = append(protocols, p)
			}
		}
		h := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")) //nolint:gosec
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(h[:]) + "\r\n"
		if len(protocols) > 0 {
			resp += "Sec-WebSocket-Protocol: " + protocols[0] + "\r\n"
		}
		_, _ = rw.WriteString(resp + "\r\n")
		_ = rw.Flush()
		s.mu.Lock()
		s.requests = append(s.requests, r)
		s.protocols = protocols
		s.conn = conn
		s.mu.Unlock()
		once.Do(func() { close(s.connected) })
		reader := bufio.NewReader(rw)
		for {
			f, err := r88provcReadFrame(reader)
			if err != nil {
				return
			}
			s.mu.Lock()
			s.frames = append(s.frames, f)
			s.mu.Unlock()
			if f.opcode == 0x8 {
				return
			}
		}
	}))
	t.Cleanup(func() {
		s.mu.Lock()
		if s.conn != nil {
			_ = s.conn.Close()
		}
		s.mu.Unlock()
		s.ts.Close()
	})
	return s
}

func r88provcReadFrame(r *bufio.Reader) (r88provcFrame, error) {
	var head [2]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return r88provcFrame{}, err
	}
	n := uint64(head[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return r88provcFrame{}, err
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return r88provcFrame{}, err
		}
		n = binary.BigEndian.Uint64(b[:])
	}
	var mask [4]byte
	if head[1]&0x80 != 0 {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return r88provcFrame{}, err
		}
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return r88provcFrame{}, err
	}
	for i := range data {
		data[i] ^= mask[i%4]
	}
	return r88provcFrame{opcode: head[0] & 0x0f, data: data}, nil
}

func (s *r88provcWS) wait(t *testing.T) {
	t.Helper()
	select {
	case <-s.connected:
	case <-time.After(5 * time.Second):
		t.Fatal("no WebSocket connection")
	}
}

func (s *r88provcWS) write(t *testing.T, b []byte) {
	t.Helper()
	s.wait(t)
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if _, err := conn.Write(b); err != nil {
		t.Fatal(err)
	}
}

func (s *r88provcWS) message(t *testing.T, v interface{}) {
	t.Helper()
	b, _ := json.Marshal(v)
	s.text(t, string(b))
}

func (s *r88provcWS) text(t *testing.T, text string) {
	t.Helper()
	frame := []byte{0x81}
	if len(text) < 126 {
		frame = append(frame, byte(len(text)))
	} else {
		frame = append(frame, 126, byte(len(text)>>8), byte(len(text)))
	}
	s.write(t, append(frame, text...))
}

func (s *r88provcWS) closeFrame(t *testing.T, code uint16, reason string) {
	t.Helper()
	p := append([]byte{byte(code >> 8), byte(code)}, reason...)
	s.write(t, append([]byte{0x88, byte(len(p))}, p...))
}

// waitFrames waits until the client sent a frame for which done is true.
func (s *r88provcWS) waitFrames(t *testing.T, done func([]r88provcFrame) bool) []r88provcFrame {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		frames := append([]r88provcFrame(nil), s.frames...)
		s.mu.Unlock()
		if done(frames) {
			return frames
		}
		if time.Now().After(deadline) {
			t.Fatalf("frames = %v", frames)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func r88provcSentCloseStream(frames []r88provcFrame) bool {
	for _, f := range frames {
		if f.opcode == 0x1 && string(f.data) == `{"type":"CloseStream"}` {
			return true
		}
	}
	return false
}

// r88provcAudio yields the given chunks, then io.EOF (or blocks when block
// is set) and records cancellation.
type r88provcAudio struct {
	mu        sync.Mutex
	chunks    [][]byte
	block     bool
	err       error
	cancelled int
}

func (a *r88provcAudio) Next(ctx context.Context) ([]byte, error) {
	a.mu.Lock()
	if len(a.chunks) > 0 {
		c := a.chunks[0]
		a.chunks = a.chunks[1:]
		a.mu.Unlock()
		return c, nil
	}
	err, block := a.err, a.block
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, io.EOF
}

func (a *r88provcAudio) Cancel(error) {
	a.mu.Lock()
	a.cancelled++
	a.mu.Unlock()
}

func (a *r88provcAudio) cancels() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cancelled
}

func r88provcRate(v int) *int { return &v }

func r88provcStreamModel(s *r88provcWS, cfg Config) *TranscriptionModel {
	if cfg.APIKey == "" {
		cfg.APIKey = "test-key"
	}
	cfg.BaseURL = s.ts.URL
	return NewTranscriptionModel(New(cfg), "nova-3")
}

func r88provcResults(text string, isFinal bool, channel int) map[string]interface{} {
	return map[string]interface{}{
		"type": "Results", "is_final": isFinal, "speech_final": isFinal, "start": 0, "duration": 1.5,
		"channel_index": []int{channel, 2},
		"channel": map[string]interface{}{"alternatives": []interface{}{map[string]interface{}{
			"transcript": text, "languages": []string{"en"},
			"words": []interface{}{map[string]interface{}{"word": "hello", "punctuated_word": "Hello!", "start": 0.2, "end": 0.8}},
		}}},
	}
}

func r88provcDrain(stream provider.TranscriptionStream) ([]*provider.TranscriptionStreamPart, error) {
	var parts []*provider.TranscriptionStreamPart
	for {
		p, err := stream.Next()
		if err == io.EOF {
			return parts, stream.Err()
		}
		if err != nil {
			return parts, err
		}
		parts = append(parts, p)
	}
}

func r88provcDefaultAudio() *r88provcAudio {
	return &r88provcAudio{chunks: [][]byte{{1, 2}, {3, 4}, {}}}
}

var r88provcPCM = provider.AudioFormat{Type: "audio/pcm", Rate: r88provcRate(16000)}

// TS deepgram-streaming-transcription.test.ts (5b89b2560c).
func TestR88provcDeepgramLiveTranscription(t *testing.T) {
	t.Run("live endpoint, auth subprotocols, headers and streaming options", func(t *testing.T) {
		s := r88provcNewWS(t)
		m := r88provcStreamModel(s, Config{APIKey: "resolved-key", Headers: map[string]string{"X-Provider": "provider"}})
		res, err := m.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
			Audio: &r88provcAudio{block: true}, InputAudioFormat: r88provcPCM,
			Headers: map[string]string{"X-Request": "request"},
			ProviderOptions: map[string]interface{}{"deepgram": map[string]interface{}{
				"language": "fr", "redact": []interface{}{"numbers", "names"},
				"streaming": map[string]interface{}{"interimResults": true, "endpointing": false, "channels": 2, "multichannel": true, "vadEvents": true, "utteranceEndMs": 1000},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		s.wait(t)
		s.mu.Lock()
		req, protocols := s.requests[0], s.protocols
		s.mu.Unlock()
		if req.URL.Path != "/v1/listen" {
			t.Fatalf("path = %s", req.URL.Path)
		}
		want := "model=nova-3&encoding=linear16&sample_rate=16000&language=fr&redact=numbers&redact=names&interim_results=true&endpointing=false&channels=2&multichannel=true&vad_events=true&utterance_end_ms=1000"
		if req.URL.RawQuery != want {
			t.Fatalf("query = %s", req.URL.RawQuery)
		}
		if !reflect.DeepEqual(protocols, []string{"token", "resolved-key"}) {
			t.Fatalf("protocols = %v", protocols)
		}
		if req.Header.Get("Authorization") != "Token resolved-key" || req.Header.Get("X-Provider") != "provider" || req.Header.Get("X-Request") != "request" {
			t.Fatalf("headers = %v", req.Header)
		}
		body, _ := res.RequestBody.(string)
		if strings.Contains(body, "resolved-key") || !strings.HasPrefix(body, "ws://") || !strings.HasSuffix(body, "/v1/listen?"+want) {
			t.Fatalf("request body = %s", body)
		}
		_ = res.Stream.Close()
	})

	t.Run("binary audio, empty chunks skipped, CloseStream, Metadata finish", func(t *testing.T) {
		s := r88provcNewWS(t)
		res, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: r88provcDefaultAudio(), InputAudioFormat: r88provcPCM})
		if err != nil {
			t.Fatal(err)
		}
		frames := s.waitFrames(t, r88provcSentCloseStream)
		if len(frames) != 3 || frames[0].opcode != 0x2 || string(frames[0].data) != "\x01\x02" || frames[1].opcode != 0x2 || string(frames[1].data) != "\x03\x04" {
			t.Fatalf("frames = %v", frames)
		}
		s.message(t, map[string]interface{}{"type": "Metadata", "duration": 2})
		parts, err := r88provcDrain(res.Stream)
		if err != nil {
			t.Fatal(err)
		}
		last := parts[len(parts)-1]
		if last.Type != provider.TranscriptionStreamPartTypeFinish || last.FinishText != "" || *last.DurationInSeconds != 2 || !reflect.DeepEqual(last.Usage, map[string]interface{}{"seconds": 2.0}) {
			t.Fatalf("finish = %+v", last)
		}
		closeFrames := s.waitFrames(t, func(f []r88provcFrame) bool { return len(f) > 0 && f[len(f)-1].opcode == 0x8 })
		if c := closeFrames[len(closeFrames)-1].data; len(c) < 2 || binary.BigEndian.Uint16(c) != 1000 {
			t.Fatalf("close frame = %v", c)
		}
	})

	t.Run("Bearer tokens use the bearer subprotocol", func(t *testing.T) {
		for _, auth := range map[string]string{"authorization": "Bearer temporary-jwt", "Authorization": "bearer temporary-jwt"} {
			s := r88provcNewWS(t)
			res, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
				Audio: &r88provcAudio{block: true}, InputAudioFormat: r88provcPCM,
				Headers: map[string]string{"Authorization": auth},
			})
			if err != nil {
				t.Fatal(err)
			}
			s.wait(t)
			s.mu.Lock()
			protocols := s.protocols
			s.mu.Unlock()
			if !reflect.DeepEqual(protocols, []string{"bearer", "temporary-jwt"}) {
				t.Fatalf("protocols = %v", protocols)
			}
			if body, _ := res.RequestBody.(string); strings.Contains(body, "temporary-jwt") {
				t.Fatal(body)
			}
			_ = res.Stream.Close()
		}
	})

	t.Run("unsupported schemes fail before connecting", func(t *testing.T) {
		for _, auth := range []string{"Basic secret", ""} {
			s := r88provcNewWS(t)
			_, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
				Audio: &r88provcAudio{block: true}, InputAudioFormat: r88provcPCM,
				Headers: map[string]string{"authorization": auth},
			})
			var ia *providererrors.InvalidArgumentError
			if !errors.As(err, &ia) || ia.Field != "headers" || !strings.Contains(err.Error(), "must use the Token or Bearer scheme") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("%q: err = %v", auth, err)
			}
			s.mu.Lock()
			n := len(s.requests)
			s.mu.Unlock()
			if n != 0 {
				t.Fatal("connected")
			}
		}
	})

	t.Run("revisable and final transcripts, words, channels, language, raw events and usage", func(t *testing.T) {
		s := r88provcNewWS(t)
		res, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: r88provcDefaultAudio(), InputAudioFormat: r88provcPCM, IncludeRawChunks: true})
		if err != nil {
			t.Fatal(err)
		}
		s.waitFrames(t, r88provcSentCloseStream)
		s.message(t, r88provcResults("Hel", false, 0))
		s.message(t, r88provcResults("Hello!", true, 1))
		s.message(t, map[string]interface{}{"type": "Metadata", "duration": 2.5, "request_id": "request-id"})
		parts, err := r88provcDrain(res.Stream)
		if err != nil {
			t.Fatal(err)
		}
		var partial, final, finish *provider.TranscriptionStreamPart
		raws := 0
		for _, p := range parts {
			switch p.Type {
			case provider.TranscriptionStreamPartTypePartial:
				partial = p
			case provider.TranscriptionStreamPartTypeFinal:
				final = p
			case provider.TranscriptionStreamPartTypeFinish:
				finish = p
			case provider.TranscriptionStreamPartTypeRaw:
				raws++
			}
		}
		if raws != 3 {
			t.Fatalf("raw parts = %d", raws)
		}
		if partial.Text != "Hel" || *partial.StartSecond != 0 || *partial.DurationInSeconds != 1.5 || *partial.ChannelIndex != 0 || partial.EndSecond != nil ||
			!reflect.DeepEqual(partial.ProviderMetadata, map[string]interface{}{"deepgram": map[string]interface{}{"speechFinal": false}}) {
			t.Fatalf("partial = %+v", partial)
		}
		if final.Text != "Hello!" || *final.StartSecond != 0 || *final.EndSecond != 1.5 || *final.ChannelIndex != 1 || final.DurationInSeconds != nil ||
			!reflect.DeepEqual(final.ProviderMetadata, map[string]interface{}{"deepgram": map[string]interface{}{"speechFinal": true}}) {
			t.Fatalf("final = %+v", final)
		}
		if finish.FinishText != "Hello!" || !reflect.DeepEqual(finish.Segments, []provider.TranscriptSegment{{Text: "Hello!", StartSecond: 0.2, EndSecond: 0.8}}) ||
			finish.Language != "en" || *finish.DurationInSeconds != 2.5 || !reflect.DeepEqual(finish.Usage, map[string]interface{}{"seconds": 2.5}) {
			t.Fatalf("finish = %+v", finish)
		}
	})

	t.Run("finalized text only, joined; a clean close after the input finishes without usage", func(t *testing.T) {
		s := r88provcNewWS(t)
		res, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: r88provcDefaultAudio(), InputAudioFormat: r88provcPCM})
		if err != nil {
			t.Fatal(err)
		}
		s.waitFrames(t, r88provcSentCloseStream)
		s.message(t, r88provcResults("Hello", true, 0))
		s.message(t, r88provcResults("world", true, 0))
		s.message(t, r88provcResults("unfinished", false, 0))
		s.closeFrame(t, 1000, "")
		parts, err := r88provcDrain(res.Stream)
		if err != nil {
			t.Fatal(err)
		}
		finish := parts[len(parts)-1]
		if finish.FinishText != "Hello world" || *finish.DurationInSeconds != 1.5 || finish.Usage != nil {
			t.Fatalf("finish = %+v", finish)
		}
	})

	t.Run("multichannel transcripts and words are ordered chronologically", func(t *testing.T) {
		s := r88provcNewWS(t)
		res, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: r88provcDefaultAudio(), InputAudioFormat: r88provcPCM})
		if err != nil {
			t.Fatal(err)
		}
		s.waitFrames(t, r88provcSentCloseStream)
		later := r88provcResults("Second", true, 1)
		later["start"] = 5
		later["channel"] = map[string]interface{}{"alternatives": []interface{}{map[string]interface{}{"transcript": "Second", "words": []interface{}{map[string]interface{}{"word": "second", "punctuated_word": "Second", "start": 5.2, "end": 5.8}}}}}
		earlier := r88provcResults("First", true, 0)
		earlier["channel"] = map[string]interface{}{"alternatives": []interface{}{map[string]interface{}{"transcript": "First", "words": []interface{}{map[string]interface{}{"word": "first", "punctuated_word": "First", "start": 0.2, "end": 0.8}}}}}
		s.message(t, later)
		s.message(t, earlier)
		s.message(t, map[string]interface{}{"type": "Metadata", "duration": 7})
		parts, err := r88provcDrain(res.Stream)
		if err != nil {
			t.Fatal(err)
		}
		finish := parts[len(parts)-1]
		if finish.FinishText != "First Second" || !reflect.DeepEqual(finish.Segments, []provider.TranscriptSegment{{Text: "First", StartSecond: 0.2, EndSecond: 0.8}, {Text: "Second", StartSecond: 5.2, EndSecond: 5.8}}) {
			t.Fatalf("finish = %+v", finish)
		}
	})

	t.Run("order is by start before channel; empty finals are dropped; Metadata before the input ends does not finish", func(t *testing.T) {
		s := r88provcNewWS(t)
		audio := &r88provcAudio{block: true}
		res, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: audio, InputAudioFormat: r88provcPCM})
		if err != nil {
			t.Fatal(err)
		}
		s.wait(t)
		late := r88provcResults("Late", true, 0)
		late["start"] = 4
		early := r88provcResults("Early", true, 1)
		s.message(t, late)
		s.message(t, early)
		s.message(t, r88provcResults("", true, 0))
		s.message(t, map[string]interface{}{"type": "Metadata", "duration": 9})
		s.closeFrame(t, 1000, "")
		parts, err := r88provcDrain(res.Stream)
		if err == nil || err.Error() != "Deepgram streaming connection closed unexpectedly (1000)." {
			t.Fatalf("err = %v (Metadata finished the stream before the input ended?)", err)
		}
		for _, p := range parts {
			if p.Type == provider.TranscriptionStreamPartTypeFinish {
				t.Fatal("finished")
			}
		}

		s = r88provcNewWS(t)
		res, err = r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: r88provcDefaultAudio(), InputAudioFormat: r88provcPCM})
		if err != nil {
			t.Fatal(err)
		}
		s.waitFrames(t, r88provcSentCloseStream)
		s.message(t, late)
		s.message(t, early)
		s.message(t, r88provcResults("", true, 0))
		s.message(t, map[string]interface{}{"type": "Metadata", "duration": 9})
		parts, err = r88provcDrain(res.Stream)
		if err != nil {
			t.Fatal(err)
		}
		if got := parts[len(parts)-1].FinishText; got != "Early Late" {
			t.Fatalf("finish text = %q", got)
		}
	})

	t.Run("other events pass as raw chunks only", func(t *testing.T) {
		for _, raw := range []bool{false, true} {
			s := r88provcNewWS(t)
			res, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: r88provcDefaultAudio(), InputAudioFormat: r88provcPCM, IncludeRawChunks: raw})
			if err != nil {
				t.Fatal(err)
			}
			s.waitFrames(t, r88provcSentCloseStream)
			s.message(t, map[string]interface{}{"type": "SpeechStarted", "channel": []int{0}, "timestamp": 0.1})
			s.message(t, map[string]interface{}{"type": "FutureEvent", "channel": "provider-specific"})
			s.message(t, r88provcResults("Hello!", true, 0))
			s.message(t, map[string]interface{}{"type": "Metadata", "duration": 2})
			parts, err := r88provcDrain(res.Stream)
			if err != nil {
				t.Fatal(err)
			}
			raws := 0
			for _, p := range parts {
				if p.Type == provider.TranscriptionStreamPartTypeRaw {
					raws++
				}
			}
			if want := map[bool]int{false: 0, true: 4}[raw]; raws != want || parts[len(parts)-1].FinishText != "Hello!" {
				t.Fatalf("raw %v: raws = %d", raw, raws)
			}
		}
	})

	t.Run("G.711 formats", func(t *testing.T) {
		for typ, enc := range map[string]string{"audio/pcmu": "mulaw", "audio/pcma": "alaw"} {
			s := r88provcNewWS(t)
			res, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: &r88provcAudio{block: true}, InputAudioFormat: provider.AudioFormat{Type: typ, Rate: r88provcRate(8000)}})
			if err != nil {
				t.Fatal(err)
			}
			body, _ := res.RequestBody.(string)
			u, _ := url.Parse(body)
			if u.Query().Get("encoding") != enc {
				t.Fatalf("%s: %s", typ, body)
			}
			_ = res.Stream.Close()
		}
	})

	t.Run("unsupported formats, Flux models", func(t *testing.T) {
		s := r88provcNewWS(t)
		for _, f := range []provider.AudioFormat{{Type: "audio/mp3", Rate: r88provcRate(16000)}, {Type: "audio/pcm"}, {Type: "audio/pcm", Rate: r88provcRate(0)}} {
			_, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: &r88provcAudio{}, InputAudioFormat: f})
			var ia *providererrors.InvalidArgumentError
			if !errors.As(err, &ia) || ia.Field != "inputAudioFormat" || !strings.HasPrefix(err.Error(), "Deepgram streaming requires") {
				t.Fatalf("%+v: err = %v", f, err)
			}
		}
		flux := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: s.ts.URL}), "flux-general-en")
		_, err := flux.DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: &r88provcAudio{}, InputAudioFormat: r88provcPCM})
		var uf *providererrors.UnsupportedFunctionalityError
		if !errors.As(err, &uf) || !strings.Contains(err.Error(), "Flux") {
			t.Fatalf("err = %v", err)
		}
		s.mu.Lock()
		n := len(s.requests)
		s.mu.Unlock()
		if n != 0 {
			t.Fatal("connected")
		}
	})

	warnings := func(t *testing.T, opts map[string]interface{}) ([]types.Warning, string) {
		t.Helper()
		s := r88provcNewWS(t)
		res, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: &r88provcAudio{block: true}, InputAudioFormat: r88provcPCM,
			ProviderOptions: map[string]interface{}{"deepgram": opts}})
		if err != nil {
			t.Fatal(err)
		}
		defer res.Stream.Close() //nolint:errcheck
		first, err := res.Stream.Next()
		if err != nil {
			t.Fatal(err)
		}
		body, _ := res.RequestBody.(string)
		return first.Warnings, body
	}

	t.Run("batch-only options warn in schema order", func(t *testing.T) {
		w, body := warnings(t, map[string]interface{}{"summarize": "v2", "detectLanguage": true, "streaming": map[string]interface{}{"interimResults": true}})
		if len(w) != 2 || w[0].Feature != "providerOptions.deepgram.detectLanguage" || w[1].Feature != "providerOptions.deepgram.summarize" ||
			w[0].Details != "This option is not supported for Deepgram streaming transcription." || strings.Contains(body, "summarize") {
			t.Fatalf("warnings = %#v, url = %s", w, body)
		}
	})

	t.Run("explicitly disabled batch-only options do not warn", func(t *testing.T) {
		w, body := warnings(t, map[string]interface{}{"detectLanguage": false, "summarize": false, "utterances": false})
		if len(w) != 0 || strings.Contains(body, "detect_language") {
			t.Fatalf("warnings = %#v, url = %s", w, body)
		}
	})

	t.Run("invalid streaming options", func(t *testing.T) {
		s := r88provcNewWS(t)
		for _, streaming := range []map[string]interface{}{{"endpointing": -1}, {"endpointing": true}, {"channels": 0}, {"utteranceEndMs": 999}, {"interimResults": nil}} {
			_, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: &r88provcAudio{}, InputAudioFormat: r88provcPCM,
				ProviderOptions: map[string]interface{}{"deepgram": map[string]interface{}{"streaming": streaming}}})
			var ia *providererrors.InvalidArgumentError
			if !errors.As(err, &ia) {
				t.Fatalf("%v: err = %v", streaming, err)
			}
		}
	})

	t.Run("cancelling the output cancels the audio", func(t *testing.T) {
		s := r88provcNewWS(t)
		audio := &r88provcAudio{block: true}
		res, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: audio, InputAudioFormat: r88provcPCM})
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Stream.Close()
		_, _ = r88provcDrain(res.Stream)
		deadline := time.Now().Add(2 * time.Second)
		for audio.cancels() != 1 && time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
		}
		if audio.cancels() != 1 {
			t.Fatalf("cancels = %d", audio.cancels())
		}
	})

	t.Run("abort returns the cause and cancels the audio", func(t *testing.T) {
		s := r88provcNewWS(t)
		audio := &r88provcAudio{block: true}
		ctx, cancel := context.WithCancelCause(context.Background())
		res, err := r88provcStreamModel(s, Config{}).DoStream(ctx, &provider.TranscriptionStreamOptions{Audio: audio, InputAudioFormat: r88provcPCM})
		if err != nil {
			t.Fatal(err)
		}
		s.wait(t)
		cancel(errors.New("stop"))
		_, err = r88provcDrain(res.Stream)
		if err == nil || err.Error() != "stop" || audio.cancels() != 1 {
			t.Fatalf("err = %v, cancels = %d", err, audio.cancels())
		}
	})

	failure := func(t *testing.T, script func(s *r88provcWS), audio *r88provcAudio) (*r88provcAudio, error) {
		t.Helper()
		s := r88provcNewWS(t)
		if audio == nil {
			audio = &r88provcAudio{block: true}
		}
		res, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: audio, InputAudioFormat: r88provcPCM})
		if err != nil {
			t.Fatal(err)
		}
		s.wait(t)
		script(s)
		_, err = r88provcDrain(res.Stream)
		return audio, err
	}

	t.Run("failures end the stream and cancel the audio", func(t *testing.T) {
		for name, tc := range map[string]struct {
			script func(s *r88provcWS)
			want   string
		}{
			"provider": {func(s *r88provcWS) {
				s.message(t, map[string]interface{}{"type": "Error", "description": "invalid audio"})
			}, "invalid audio"},
			"provider msg":   {func(s *r88provcWS) { s.message(t, map[string]interface{}{"type": "Error", "message": "bad"}) }, "bad"},
			"provider none":  {func(s *r88provcWS) { s.message(t, map[string]interface{}{"type": "Error"}) }, "Deepgram streaming transcription failed."},
			"malformed":      {func(s *r88provcWS) { s.text(t, "invalid json") }, "invalid json"},
			"missing type":   {func(s *r88provcWS) { s.message(t, map[string]interface{}{"kind": "x"}) }, "Type validation failed"},
			"no alternative": {func(s *r88provcWS) { s.message(t, map[string]interface{}{"type": "Results"}) }, "Deepgram Results event is missing a transcript alternative."},
			"early close":    {func(s *r88provcWS) { s.closeFrame(t, 1000, "") }, "Deepgram streaming connection closed unexpectedly (1000)."},
			"abnormal close": {func(s *r88provcWS) { s.closeFrame(t, 1011, "") }, "Deepgram streaming connection closed unexpectedly (1011)."},
			"close reason":   {func(s *r88provcWS) { s.closeFrame(t, 1008, "DATA-0000") }, "Deepgram streaming connection closed unexpectedly (1008): DATA-0000"},
		} {
			audio, err := failure(t, tc.script, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) || audio.cancels() != 1 {
				t.Fatalf("%s: err = %v, cancels = %d", name, err, audio.cancels())
			}
		}
	})

	t.Run("a close code other than 1000 after the input fails", func(t *testing.T) {
		_, err := failure(t, func(s *r88provcWS) {
			s.waitFrames(t, r88provcSentCloseStream)
			s.closeFrame(t, 1011, "")
		}, r88provcDefaultAudio())
		if err == nil || err.Error() != "Deepgram streaming connection closed unexpectedly (1011)." {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("input producer errors propagate", func(t *testing.T) {
		_, err := failure(t, func(*r88provcWS) {}, &r88provcAudio{err: errors.New("microphone disconnected")})
		if err == nil || err.Error() != "microphone disconnected" {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("keepalive while waiting for audio", func(t *testing.T) {
		old := deepgramKeepAliveInterval
		deepgramKeepAliveInterval = 20 * time.Millisecond
		defer func() { deepgramKeepAliveInterval = old }()
		s := r88provcNewWS(t)
		res, err := r88provcStreamModel(s, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: &r88provcAudio{block: true}, InputAudioFormat: r88provcPCM})
		if err != nil {
			t.Fatal(err)
		}
		s.waitFrames(t, func(f []r88provcFrame) bool {
			for _, fr := range f {
				if string(fr.data) == `{"type":"KeepAlive"}` {
					return true
				}
			}
			return false
		})
		_ = res.Stream.Close()
	})

	t.Run("a missing API key fails before connecting", func(t *testing.T) {
		t.Setenv("DEEPGRAM_API_KEY", "")
		s := r88provcNewWS(t)
		m := NewTranscriptionModel(New(Config{BaseURL: s.ts.URL}), "nova-3")
		_, err := m.DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: &r88provcAudio{}, InputAudioFormat: r88provcPCM})
		if !providererrors.IsLoadAPIKeyError(err) {
			t.Fatalf("err = %v", err)
		}
	})
}

// TS deepgram-transcription-model.ts (5b89b2560c): batch transcription warns
// about streaming options.
func TestR88provcDeepgramBatchStreamingWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{"duration":1},"results":{"channels":[{"alternatives":[{"transcript":"hi","words":[]}]}]}}`))
	}))
	defer srv.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "nova-3")
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"deepgram": map[string]interface{}{"streaming": map[string]interface{}{"interimResults": true}}}})
	if err != nil {
		t.Fatal(err)
	}
	want := []types.Warning{{Type: "unsupported", Feature: "providerOptions.deepgram.streaming", Details: "Deepgram batch transcription does not support streaming options."}}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Fatalf("warnings = %#v", res.Warnings)
	}
}
