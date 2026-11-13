package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"golang.org/x/net/websocket"
)

// TestR97provbChatFractionalUsageRawExact: TS chat usage counts are
// z.number() and usage.raw is the parsed usage object, so a fractional
// count stays exact in Usage.Raw (Go's raw held the truncated retry value).
func TestR97provbChatFractionalUsageRawExact(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10.5,"completion_tokens":3,"total_tokens":13.5,"prompt_tokens_details":{"cached_tokens":1.5}}}`))
	}))
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).ChatModel("gpt-4o")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if *res.Usage.InputTokens != 10 {
		t.Fatalf("input tokens = %d, want 10", *res.Usage.InputTokens)
	}
	details, _ := res.Usage.Raw["prompt_tokens_details"].(map[string]interface{})
	if res.Usage.Raw["prompt_tokens"] != 10.5 || details["cached_tokens"] != 1.5 {
		t.Fatalf("raw = %#v, want the exact fractional counts", res.Usage.Raw)
	}
}

// r97provbNullMessageServer accepts the realtime handshake, reads the
// session update and sends a JSON null text message.
func r97provbNullMessageServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(websocket.Server{Handshake: func(cfg *websocket.Config, _ *http.Request) error {
		if len(cfg.Protocol) > 0 {
			cfg.Protocol = cfg.Protocol[:1]
		}
		return nil
	}, Handler: func(ws *websocket.Conn) {
		var msg string
		_ = websocket.Message.Receive(ws, &msg)
		_ = websocket.Message.Send(ws, "null")
		_ = websocket.Message.Receive(ws, &msg)
	}})
	t.Cleanup(ts.Close)
	return ts
}

// TestR97provbRealtimeNullMessageTypeError: TS safeParseJSON parses a JSON
// null message, then reads raw.type of it, and the TypeError fails the
// stream through onProcessingError (after the raw part). Go treated it as
// an object without a type and kept the stream open.
func TestR97provbRealtimeNullMessageTypeError(t *testing.T) {
	const want = "Cannot read properties of null (reading 'type')"
	ts := r97provbNullMessageServer(t)
	rate := 24000
	tr, err := newTestTranscriptionModel(ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: newChanTestAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"}, IncludeRawChunks: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Stream.Close() //nolint:errcheck
	var parts []string
	for {
		p, err := tr.Stream.Next()
		if err != nil {
			if err.Error() != want {
				t.Errorf("transcription: err = %v, want %q", err, want)
			}
			break
		}
		parts = append(parts, string(p.Type))
	}
	if strings.Join(parts, ",") != "stream-start,raw" {
		t.Errorf("transcription parts = %v", parts)
	}

	st, err := newTestSpeechTranslationModel(ts.URL).DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{Audio: newChanTestAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate}, TargetLanguage: "es"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Stream.Close() //nolint:errcheck
	for {
		_, err := st.Stream.Next()
		if err != nil {
			if err.Error() != want {
				t.Errorf("translation: err = %v, want %q", err, want)
			}
			break
		}
	}
}

// TestR97provbCompletionUsageRaw: TS convertOpenAICompletionUsage returns
// raw: usage, the schema output. The OpenAI z.object keeps the three counts
// as sent (a fraction included); the openai-compatible z.looseObject keeps
// every key. Go rebuilt raw from its truncated int fields.
func TestR97provbCompletionUsageRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"text":"hi","finish_reason":"stop","index":0}],"usage":{"prompt_tokens":10.5,"completion_tokens":3,"total_tokens":13.5,"cost":0.25}}`))
	}))
	defer srv.Close()
	for _, compat := range []bool{false, true} {
		m, _ := New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: compat}).CompletionModel("gpt-3.5-turbo-instruct")
		res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		if *res.Usage.InputTokens != 10 || res.Usage.Raw["prompt_tokens"] != 10.5 || res.Usage.Raw["total_tokens"] != 13.5 {
			t.Errorf("compat %v: input = %d, raw = %#v", compat, *res.Usage.InputTokens, res.Usage.Raw)
		}
		if _, has := res.Usage.Raw["cost"]; has != compat {
			t.Errorf("compat %v: raw = %#v, want cost only for the loose openai-compatible schema", compat, res.Usage.Raw)
		}
	}
}

// TestR97provbLiveServerEventInfinity: the OpenAI Live server event schemas
// use z.number() (nonnegative), which rejects Infinity; a number beyond the
// double range reads as +Inf (main ruling 4), so the event is the
// invalid_server_event error (Go accepted it).
func TestR97provbLiveServerEventInfinity(t *testing.T) {
	for _, frame := range []string{
		`{"type":"session.usage.updated","usage":{"seconds":1e400}}`,
		`{"type":"session.output_transcript.delta","delta":"x","start_ms":0,"end_ms":1e400}`,
		`{"type":"session.delegation.created","delegation":{"id":"d"},"offset_ms":1e400}`,
	} {
		events := parseOpenAILiveServerEvent([]byte(frame))
		if len(events) != 1 || events[0].Type != "error" || events[0].Code != "invalid_server_event" {
			t.Errorf("%s: events = %+v, want invalid_server_event", frame, events)
		}
	}
}

// TestR97provbApprovalRequestIDEmptyKey: TS
// extractApprovalRequestIdToToolCallIdMapping maps every approvalRequestId
// that is != null, a present "" included.
func TestR97provbApprovalRequestIDEmptyKey(t *testing.T) {
	m := extractApprovalRequestIDToToolCallIDFromPrompt(types.Prompt{Messages: []types.Message{{
		Role: types.RoleAssistant,
		Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "tc1", ToolName: "mcp.x",
			ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"approvalRequestId": ""}}}},
	}}})
	if m[""] != "tc1" {
		t.Fatalf("mapping = %#v, want \"\" -> tc1", m)
	}
}

// TestR97provbRealtimeParseNullEvent: TS parseOpenAIRealtimeServerEvent
// reads event.type of the parsed message, so a JSON null throws a TypeError
// (Go returned a custom event).
func TestR97provbRealtimeParseNullEvent(t *testing.T) {
	_, err := NewRealtimeModel(New(Config{APIKey: "k"}), "gpt-realtime").ParseServerEvent([]byte("null"))
	if err == nil || err.Error() != "Cannot read properties of null (reading 'type')" {
		t.Fatalf("err = %v, want the TypeError", err)
	}
}
