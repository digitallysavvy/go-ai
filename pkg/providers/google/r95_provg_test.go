package google

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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
