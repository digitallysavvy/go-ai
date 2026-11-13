package google

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type r95provgLooseTarget struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// looseDecode (the interactions unknownEvent fallback) reads the payload
// with exact keys, as the strict decode and zod do: "Name" is not name.
func TestR95provgLooseDecodeKeysMatchExactly(t *testing.T) {
	var v r95provgLooseTarget
	looseDecode([]byte(`{"Name":"wrong case","count":2}`), &v)
	if v.Name != "" || v.Count != 2 {
		t.Fatalf("decoded %+v, want name empty and count 2", v)
	}
}

// r95provgHangingServer accepts TCP connections and never answers, so a
// WebSocket handshake stays pending until the caller aborts.
func r95provgHangingServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
	})
	return "http://" + ln.Addr().String() + "/v1beta"
}

func r95provgFirstErr(t *testing.T, next func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		for {
			if err := next(); err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not end")
		return nil
	}
}

// TS connectToWebSocket onAbort(abortSignal.reason): an abort while the
// handshake is pending fails the stream with the reason itself, not a
// "websocket dial ...: reason" error around it.
func TestR95provgLiveStreamsAbortDuringDialReportReason(t *testing.T) {
	reason := errors.New("r95provg user stop")
	base := r95provgHangingServer(t)

	ctx, cancel := context.WithCancelCause(context.Background())
	res, err := newLiveTestModel(t, base).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio: newBlockingAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Stream.Close() //nolint:errcheck
	time.Sleep(50 * time.Millisecond)
	cancel(reason)
	if got := r95provgFirstErr(t, func() error { _, err := res.Stream.Next(); return err }); got != reason {
		t.Errorf("transcription: err = %v, want the abort reason itself", got)
	}

	ctx2, cancel2 := context.WithCancelCause(context.Background())
	sres, err := newSpeechTranslationTestModel(t, base, 0).DoStream(ctx2, &provider.SpeechTranslationStreamOptions{
		Audio: newBlockingAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"}, TargetLanguage: "es",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sres.Stream.Close() //nolint:errcheck
	time.Sleep(50 * time.Millisecond)
	cancel2(reason)
	if got := r95provgFirstErr(t, func() error { _, err := sres.Stream.Next(); return err }); got != reason {
		t.Errorf("speech translation: err = %v, want the abort reason itself", got)
	}
}

func r95provgRefusingServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1beta"
}

// TS onSocketError: a WebSocket that fails to open (a 401 handshake) fails
// the stream with the provider's socket error text; a signal aborted before
// the open fails it with the reason.
func TestR95provgLiveStreamsRefusedHandshakeAndPreAbort(t *testing.T) {
	base := r95provgRefusingServer(t)
	res, err := newLiveTestModel(t, base).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio: newBlockingAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := r95provgFirstErr(t, func() error { _, err := res.Stream.Next(); return err }); got == nil || got.Error() != "Google Live transcription error" {
		t.Errorf("transcription: err = %v", got)
	}
	sres, err := newSpeechTranslationTestModel(t, base, 0).DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio: newBlockingAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"}, TargetLanguage: "es",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := r95provgFirstErr(t, func() error { _, err := sres.Stream.Next(); return err }); got == nil || got.Error() != "Google Live translation error" {
		t.Errorf("speech translation: err = %v", got)
	}

	reason := errors.New("r95provg aborted before open")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(reason)
	res, err = newLiveTestModel(t, base).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio: newBlockingAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err == nil {
		if got := r95provgFirstErr(t, func() error { _, err := res.Stream.Next(); return err }); got != reason {
			t.Errorf("pre-aborted transcription: err = %v, want the reason", got)
		}
	} else if err != reason {
		t.Errorf("pre-aborted transcription: DoStream err = %v", err)
	}
}

// TS cancel() / onAbort run cleanup(), which cancels the caller's audio:
// also when the consumer closes the stream while a part (here stream-start)
// is still waiting to be read.
func TestR95provgLiveCloseWhileEmitPendingCancelsAudio(t *testing.T) {
	for _, name := range []string{"transcription", "translation"} {
		t.Run(name, func(t *testing.T) {
			server := newLiveTranscriptionTestServer(t)
			defer server.close()
			audio := newBlockingAudioStream()
			var stream interface{ Close() error }
			if name == "transcription" {
				res, err := newLiveTestModel(t, server.ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
					Audio: audio, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
				})
				if err != nil {
					t.Fatal(err)
				}
				stream = res.Stream
			} else {
				res, err := newSpeechTranslationTestModel(t, server.ts.URL, 0).DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
					Audio: audio, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"}, TargetLanguage: "es",
				})
				if err != nil {
					t.Fatal(err)
				}
				stream = res.Stream
			}
			server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, 2*time.Second)
			time.Sleep(20 * time.Millisecond)
			_ = stream.Close()
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if c, _ := audio.wasCancelled(); c {
					return
				}
				time.Sleep(2 * time.Millisecond)
			}
			t.Fatal("audio not cancelled after Close with a pending part")
		})
	}
}

// TS finishes on turnComplete only when interactionStatus == null; a
// present "" status is not null, so the stream keeps going.
func TestR95provgLiveEmptyInteractionStatusDoesNotFinish(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()
	res, err := newLiveTestModel(t, server.ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio: newChanAudioStream([]byte{1, 2}), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Stream.Close() //nolint:errcheck
	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, 2*time.Second)
	if _, err := res.Stream.Next(); err != nil { // stream-start
		t.Fatal(err)
	}
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	server.waitFor(t, hasRealtimeInputKey("audioStreamEnd"), 2*time.Second)
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"turnComplete": true, "interactionStatus": ""}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"inputTranscription": map[string]interface{}{"text": "late", "finished": true}}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"interactionStatus": "IDLE"}}
	parts, err := drainUntilFinishOrError(t, res.Stream)
	if err != nil {
		t.Fatal(err)
	}
	last := parts[len(parts)-1]
	if last.Type != provider.TranscriptionStreamPartTypeFinish || last.FinishText != "late" {
		t.Fatalf("finish = %+v, want text \"late\"", last)
	}
}

// r95provgEventStream runs an Interactions SSE body through the stream
// transform and returns its chunks.
func r95provgEventStream(t *testing.T, events ...string) []*provider.StreamChunk {
	t.Helper()
	body := ""
	for _, e := range events {
		body += "data: " + e + "\n\n"
	}
	st := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	var chunks []*provider.StreamChunk
	for {
		c, err := st.Next()
		if err != nil {
			break
		}
		chunks = append(chunks, c)
	}
	return chunks
}

func r95provgChunk(chunks []*provider.StreamChunk, typ provider.ChunkType) *provider.StreamChunk {
	for _, c := range chunks {
		if c.Type == typ {
			return c
		}
	}
	return nil
}

// TS `event.status != null` / `interaction.status != null` and
// `error?.code ?? undefined`: a present "" status or code is a value.
func TestR95provgInteractionsStreamEmptyStatusAndCode(t *testing.T) {
	chunks := r95provgEventStream(t, `{"event_type":"interaction.status_update","status":""}`)
	if f := r95provgChunk(chunks, provider.ChunkTypeFinish); f == nil || f.FinishReason != types.FinishReasonOther || f.RawFinishReason != "" {
		t.Fatalf("status_update \"\" finish = %+v", f)
	}
	chunks = r95provgEventStream(t,
		`{"event_type":"interaction.requires_action"}`,
		`{"event_type":"interaction.completed","interaction":{"status":""}}`)
	if f := r95provgChunk(chunks, provider.ChunkTypeFinish); f == nil || f.FinishReason != types.FinishReasonOther {
		t.Fatalf("completed \"\" finish = %+v", f)
	}
	chunks = r95provgEventStream(t, `{"event_type":"error","error":{"code":"","message":"m"}}`)
	e := r95provgChunk(chunks, provider.ChunkTypeError)
	var spe *providererrors.StreamProviderError
	if e == nil || !errors.As(e.Err, &spe) || spe.Code != "" {
		t.Fatalf("error chunk = %+v", e)
	}
	if code, ok := spe.Code.(string); !ok || code != "" {
		t.Fatalf("code = %#v, want present \"\"", spe.Code)
	}
}

// TS: a processing block takes either processing delta type, so a
// processing_result delta on a processing_call block adds processingCallId.
func TestR95provgInteractionsProcessingDeltaEitherType(t *testing.T) {
	chunks := r95provgEventStream(t,
		`{"event_type":"step.start","index":0,"step":{"type":"processing_call","id":"p1"}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"processing_result","call_id":"c9","signature":"s"}}`,
		`{"event_type":"step.stop","index":0}`)
	c := r95provgChunk(chunks, provider.ChunkTypeCustom)
	if c == nil {
		t.Fatal("no custom chunk")
	}
	meta := string(c.ProviderMetadata)
	for _, want := range []string{`"processingId":"p1"`, `"processingCallId":"c9"`, `"signature":"s"`} {
		if !strings.Contains(meta, want) {
			t.Fatalf("metadata %s lacks %s", meta, want)
		}
	}
}

// TS parseGoogleInteractionsOutputs: a built-in tool call has no
// providerMetadata, in the generate tool calls and the synthesized stream.
func TestR95provgInteractionsBuiltinToolCallNoMetadata(t *testing.T) {
	var resp interactionsResponse
	if err := json.Unmarshal([]byte(`{"id":"i1","status":"completed","steps":[{"type":"google_search_call","id":"g1","arguments":{"queries":["q"]}}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	m := &InteractionsLanguageModel{}
	_, calls, _ := m.parseOutputs(resp.Steps, "i1")
	if len(calls) != 1 || calls[0].ProviderMetadata != nil {
		t.Fatalf("tool calls = %+v", calls)
	}
	st := newSynthesizedInteractionsStream(m, resp, nil, nil, false)
	var call *provider.StreamChunk
	for {
		c, err := st.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeToolCall {
			call = c
		}
	}
	if call == nil || call.ToolCall.ProviderMetadata != nil || len(call.ProviderMetadata) != 0 {
		t.Fatalf("synthesized tool call = %+v", call)
	}
}

// googleInteractionsLanguageModelOptions: enum and union checks.
func TestR95provgInteractionsOptionSchema(t *testing.T) {
	bad := map[string]map[string]interface{}{
		"empty aspectRatio":       {"imageConfig": map[string]interface{}{"aspectRatio": ""}},
		"numeric imageSize":       {"imageConfig": map[string]interface{}{"imageSize": 5}},
		"empty agent type":        {"agentConfig": map[string]interface{}{"type": ""}},
		"unknown agent type":      {"agentConfig": map[string]interface{}{"type": "other"}},
		"empty visualization":     {"agentConfig": map[string]interface{}{"type": "deep-research", "visualization": ""}},
		"numeric thinkingSummary": {"agentConfig": map[string]interface{}{"type": "deep-research", "thinkingSummaries": 1}},
		"string collaborative":    {"agentConfig": map[string]interface{}{"type": "deep-research", "collaborativePlanning": "x"}},
		"non-string modality":     {"responseModalities": []interface{}{1}},
		"transform not array":     {"environment": map[string]interface{}{"type": "remote", "network": map[string]interface{}{"allowlist": []interface{}{map[string]interface{}{"domain": "d", "transform": "x"}}}}},
		"transform number value":  {"environment": map[string]interface{}{"type": "remote", "network": map[string]interface{}{"allowlist": []interface{}{map[string]interface{}{"domain": "d", "transform": []interface{}{map[string]interface{}{"a": 1}}}}}}},
	}
	for name, opts := range bad {
		if _, err := parseInteractionsProviderOptions(map[string]interface{}{"google": opts}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	good := map[string]map[string]interface{}{
		"loose dynamic": {"agentConfig": map[string]interface{}{"type": "dynamic", "collaborativePlanning": "x", "thinkingSummaries": "bogus"}},
		"deep-research": {"agentConfig": map[string]interface{}{"type": "deep-research", "visualization": "auto", "collaborativePlanning": true}},
		"transform":     {"environment": map[string]interface{}{"type": "remote", "network": map[string]interface{}{"allowlist": []interface{}{map[string]interface{}{"domain": "d", "transform": []interface{}{map[string]interface{}{"a": "b"}}}}}}},
	}
	for name, opts := range good {
		if _, err := parseInteractionsProviderOptions(map[string]interface{}{"google": opts}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TS streamGoogleInteractionEvents cancels the run only when the stream ends
// through an abort: a stream that failed on its own leaves it going, an abort
// mid-read cancels once, and the cancel goes to the id the stream opened.
func TestR95provgInteractionsBackgroundStreamCancelOnlyOnAbort(t *testing.T) {
	old := interactionsStreamRetryDelay
	interactionsStreamRetryDelay = time.Millisecond
	t.Cleanup(func() { interactionsStreamRetryDelay = old })

	var mu sync.Mutex
	var cancels []string
	failGET := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/cancel") {
			mu.Lock()
			cancels = append(cancels, r.URL.Path)
			mu.Unlock()
			return
		}
		mu.Lock()
		fail := failGET
		mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"code":500,"message":"down","status":"INTERNAL"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"event_type":"interaction.created","interaction":{"id":"other"}}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	client := New(Config{APIKey: "k", BaseURL: server.URL}).client
	count := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), cancels...)
	}

	st, _ := newInteractionsStream(context.Background(), client, "i1", nil, nil, nil, 0)
	if _, err := st.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Next(); err == nil {
		t.Fatal("want a stream error")
	}
	_ = st.Close()
	if got := count(); len(got) != 0 {
		t.Fatalf("failed stream sent cancel: %v", got)
	}

	mu.Lock()
	failGET = false
	mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	st, _ = newInteractionsStream(ctx, client, "i1", nil, nil, nil, 0)
	if _, err := st.Next(); err != nil { // stream-start
		t.Fatal(err)
	}
	if c, err := st.Next(); err != nil || c.Type != provider.ChunkTypeResponseMetadata {
		t.Fatalf("metadata = %+v, %v", c, err)
	}
	cancel()
	if _, err := st.Next(); err == nil {
		t.Fatal("want the abort")
	}
	if got := count(); len(got) != 1 || got[0] != "/interactions/i1/cancel" {
		t.Fatalf("abort cancels = %v", got)
	}
	_ = st.Close()
	if got := count(); len(got) != 1 {
		t.Fatalf("cancels after Close = %v", got)
	}
}
