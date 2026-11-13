package openai

import (
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

var errR96provbWrite = errors.New("r96provb write failed")

func (c *r96provbFailingWritesConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.read.Store(true)
	return n, err
}

func (c *r96provbFailingWritesConn) Write(b []byte) (int, error) {
	if c.read.Load() {
		return 0, errR96provbWrite
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
			if !errors.Is(err, errR96provbWrite) {
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
			if !errors.Is(err, errR96provbWrite) {
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

// TestR96provbResponsesEmptyReasoningOptions: reasoningEffort and
// reasoningSummary are z.string() Responses options, so a present "" is
// sent (TS resolvedReasoningEffort / resolvedReasoningSummary `!= null`):
// effort "" also defaults the summary to "detailed", and a non-reasoning
// model warns for each.
func TestR96provbResponsesEmptyReasoningOptions(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "o3")
	body, _, err := m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": ""}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := body["reasoning"].(map[string]interface{}); r == nil || r["effort"] != "" || r["summary"] != "detailed" {
		t.Errorf("effort \"\": reasoning = %#v, want {effort: \"\", summary: detailed}", body["reasoning"])
	}
	body, _, err = m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningSummary": ""}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := body["reasoning"].(map[string]interface{}); r == nil || r["summary"] != "" {
		t.Errorf("summary \"\": reasoning = %#v, want {summary: \"\"}", body["reasoning"])
	}
	_, _, warnings, err := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-4o").buildRequest(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": "", "reasoningSummary": ""}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, w := range warnings {
		got[w.Feature] = true
	}
	if !got["reasoningEffort"] || !got["reasoningSummary"] {
		t.Errorf("non-reasoning model warnings = %+v, want reasoningEffort and reasoningSummary", warnings)
	}
}

// TestR96provbStreamFunctionCallDoneItemVerbatim: TS builds the tool-call
// from the done item's id, call_id, name and arguments verbatim (required
// strings), so present "" values are not replaced by the added item's or
// the accumulated deltas.
func TestR96provbStreamFunctionCallDoneItemVerbatim(t *testing.T) {
	stream := newResponsesStreamWithMetadata(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"weather","arguments":"","status":"in_progress"}}

data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"{\"a\":1}"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"","call_id":"","name":"","arguments":"","status":"completed"}}

`)), false, "web_search", "openai", nil)
	defer stream.Close() //nolint:errcheck
	for {
		c, err := stream.Next()
		if err != nil {
			t.Fatal("no tool-call chunk")
		}
		if c.Type == provider.ChunkTypeToolCall {
			meta, _ := c.ToolCall.ProviderMetadata["openai"].(map[string]interface{})
			if c.ToolCall.ID != "" || c.ToolCall.ToolName != "" || c.ToolCall.RawArguments != "" || meta["itemId"] != "" {
				t.Fatalf("tool call = %+v, want the done item's empty values", c.ToolCall)
			}
			return
		}
	}
}

// TestR96provbFractionalNumberOptions: z.number() options keep a fraction,
// so TS sends maxToolCalls, logprobs (top_logprobs) and maxCompletionTokens
// as given; Go truncated them to integers.
func TestR96provbFractionalNumberOptions(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-4o")
	body, _, err := m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"maxToolCalls": 2.5, "logprobs": 3.5}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if body["max_tool_calls"] != 2.5 || body["top_logprobs"] != 3.5 {
		t.Errorf("responses body max_tool_calls = %#v, top_logprobs = %#v, want 2.5 and 3.5", body["max_tool_calls"], body["top_logprobs"])
	}
	if inc, _ := body["include"].([]string); len(inc) != 1 || inc[0] != "message.output_text.logprobs" {
		t.Errorf("include = %#v, want the logprobs include", body["include"])
	}
	body, _, err = m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"maxToolCalls": 3.0}}}, false)
	if err != nil || body["max_tool_calls"] != 3 {
		t.Errorf("integral max_tool_calls = %#v (%v), want int 3", body["max_tool_calls"], err)
	}
	chat := NewLanguageModel(New(Config{APIKey: "k"}), "gpt-4o")
	cbody, _, err := chat.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"maxCompletionTokens": 100.5, "logprobs": 2.5}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if cbody["max_completion_tokens"] != 100.5 || cbody["top_logprobs"] != 2.5 {
		t.Errorf("chat body max_completion_tokens = %#v, top_logprobs = %#v, want 100.5 and 2.5", cbody["max_completion_tokens"], cbody["top_logprobs"])
	}
}

// TestR96provbStreamEmptyIDsKept: TS records response.id and an added
// message's id as given (required strings), so a present "" is the finish
// responseId and the text-end id, not null / the done item's id.
func TestR96provbStreamEmptyIDsKept(t *testing.T) {
	stream := newResponsesStreamWithMetadata(io.NopCloser(strings.NewReader(`data: {"type":"response.created","response":{"id":"","created_at":1,"model":"m"}}

data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"","role":"assistant","status":"in_progress","content":[]}}

data: {"type":"response.output_text.delta","item_id":"x","output_index":0,"content_index":0,"delta":"hi"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"x","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi","annotations":[]}]}}

data: {"type":"response.completed","response":{"id":"","created_at":1,"model":"m","status":"completed","output":[]}}

`)), false, "web_search", "openai", nil)
	defer stream.Close() //nolint:errcheck
	var textEndID *string
	for {
		c, err := stream.Next()
		if err != nil {
			t.Fatal("no finish chunk")
		}
		if c.Type == provider.ChunkTypeTextEnd {
			id := c.ID
			textEndID = &id
		}
		if c.Type == provider.ChunkTypeFinish {
			if textEndID == nil || *textEndID != "" {
				t.Errorf("text-end id = %v, want \"\"", textEndID)
			}
			if !strings.Contains(string(c.ProviderMetadata), `"responseId":""`) {
				t.Errorf("finish metadata = %s, want responseId \"\"", c.ProviderMetadata)
			}
			return
		}
	}
}
