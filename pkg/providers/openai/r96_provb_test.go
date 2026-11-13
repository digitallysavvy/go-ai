package openai

import (
	"context"
	"errors"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"golang.org/x/net/websocket"
)

// r96provbHTML is a provider string with the characters encoding/json
// escapes as <, > and & and JSON.stringify writes as is.
const r96provbHTML = "<a&b>"

// TestR96provbMetadataJSONStringify: provider metadata the SDK serializes
// from provider values is written like JSON.stringify (jsonstr), so <, >
// and & stay literal (main ruling 4, write side: the same serializer maps a
// non-finite number to null).
func TestR96provbMetadataJSONStringify(t *testing.T) {
	for name, raw := range map[string][]byte{
		"toRawMetadata":     toRawMetadata(map[string]interface{}{"openai": map[string]interface{}{"itemId": r96provbHTML}}),
		"reasoningMetadata": openAIResponsesReasoningMetadataWithEncrypted("openai", r96provbHTML, nil),
		"marshalOrNil":      marshalOrNil(map[string]interface{}{"openai": map[string]interface{}{"itemId": r96provbHTML}}),
		"completion":        mustMarshalCompletionProviderMetadata(map[string]interface{}{"openai": map[string]interface{}{"id": r96provbHTML}}),
		"logprobs":          normalizeResponsesLogprobs([]byte(`[{"token":"<a&b>","logprob":-1,"top_logprobs":[]}]`)),
	} {
		if !strings.Contains(string(raw), r96provbHTML) {
			t.Errorf("%s: %s, want %q written literally", name, raw, r96provbHTML)
		}
	}
}

// r96provbFailingWritesConn fails every write once the handshake response has
// been read, so the first frame sent after the socket opens fails.
type r96provbFailingWritesConn struct {
	net.Conn
	read atomic.Bool
}

var r96provbWriteErr = errors.New("r96provb write failed")

func (c *r96provbFailingWritesConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.read.Store(true)
	return n, err
}

func (c *r96provbFailingWritesConn) Write(b []byte) (int, error) {
	if c.read.Load() {
		return 0, r96provbWriteErr
	}
	return c.Conn.Write(b)
}

// TestR96provbSetupSendFailureAfterStreamStart (cr-r96-prova-to-provb): TS
// onOpen enqueues stream-start before it sends the session update, so a
// failed send errors the stream after stream-start (Go returned before it).
func TestR96provbSetupSendFailureAfterStreamStart(t *testing.T) {
	prev := openAIRealtimeWrapConn
	openAIRealtimeWrapConn = func(c net.Conn) net.Conn { return &r96provbFailingWritesConn{Conn: c} }
	t.Cleanup(func() { openAIRealtimeWrapConn = prev })
	ts := httptest.NewServer(websocket.Server{Handshake: func(cfg *websocket.Config, _ *http.Request) error {
		if len(cfg.Protocol) > 0 {
			cfg.Protocol = cfg.Protocol[:1] // echo the requested subprotocol
		}
		return nil
	}, Handler: func(ws *websocket.Conn) {
		var msg string
		_ = websocket.Message.Receive(ws, &msg)
	}})
	defer ts.Close()
	rate := 24000

	tr, err := newTestTranscriptionModel(ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: newChanTestAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"}})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Stream.Close() //nolint:errcheck
	var trParts []string
	for {
		p, err := tr.Stream.Next()
		if err != nil {
			if !errors.Is(err, r96provbWriteErr) {
				t.Errorf("transcription: err = %v, want the send error", err)
			}
			break
		}
		trParts = append(trParts, string(p.Type))
	}
	if len(trParts) != 1 || trParts[0] != provider.TranscriptionStreamPartTypeStreamStart {
		t.Errorf("transcription parts = %v, want stream-start before the error", trParts)
	}

	st, err := newTestSpeechTranslationModel(ts.URL).DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{Audio: newChanTestAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate}, TargetLanguage: "es"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Stream.Close() //nolint:errcheck
	var stParts []string
	for {
		p, err := st.Stream.Next()
		if err != nil {
			if !errors.Is(err, r96provbWriteErr) {
				t.Errorf("translation: err = %v, want the send error", err)
			}
			break
		}
		stParts = append(stParts, string(p.Type))
	}
	if len(stParts) != 1 || stParts[0] != provider.SpeechTranslationStreamPartTypeStreamStart {
		t.Errorf("translation parts = %v, want stream-start before the error", stParts)
	}
}

// TestR96provbFilesExpiresAfterInfinity: zod 4 z.number() rejects
// +-Infinity, so providerOptions.openai.expiresAfter = +Inf is an invalid
// option (Go sent "expires_after[seconds]=Infinity").
func TestR96provbFilesExpiresAfterInfinity(t *testing.T) {
	for _, v := range []float64{math.Inf(1), math.Inf(-1)} {
		if _, err := parseOpenAIFilesOptions(map[string]interface{}{"openai": map[string]interface{}{"expiresAfter": v}}); err == nil {
			t.Errorf("expiresAfter %v: no error", v)
		}
	}
	if _, err := parseOpenAIFilesOptions(map[string]interface{}{"openai": map[string]interface{}{"expiresAfter": 3600.0}}); err != nil {
		t.Errorf("expiresAfter 3600: %v", err)
	}
}
