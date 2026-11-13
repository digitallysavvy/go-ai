package google

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS google-files.ts reads the upload result with uploadResponse.json()
// (plain JSON.parse, not secureJsonParse): a __proto__ key or a
// constructor.prototype is an ordinary property and the upload succeeds.
func TestR97provgFilesUploadResultAcceptsPrototypeKeys(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/v1beta/files":
			w.Header().Set("x-goog-upload-url", srv.URL+"/upload-session")
		case "/upload-session":
			_, _ = w.Write([]byte(`{"__proto__":{"x":1},"file":{"name":"files/a","mimeType":"application/pdf","uri":"https://g/files/a","state":"ACTIVE","constructor":{"prototype":{}}}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		MediaType: "application/pdf",
	})
	if err != nil {
		t.Fatalf("UploadFile() err = %v, want success (TS JSON.parse accepts the keys)", err)
	}
	if res.ProviderReference["google"] != "https://g/files/a" {
		t.Fatalf("reference = %v", res.ProviderReference)
	}
}

// TS doCreateClientSecret reads the auth token with response.json() (plain
// JSON.parse): a __proto__ key does not fail the token.
func TestR97provgRealtimeClientSecretAcceptsPrototypeKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"auth_tokens/t1","__proto__":{"a":1}}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})
	m := &GoogleRealtimeModel{provider: p, modelID: "gemini-live-2.5-flash"}
	res, err := m.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	if err != nil {
		t.Fatalf("DoCreateClientSecret err = %v, want success", err)
	}
	if res.Token != "auth_tokens/t1" {
		t.Fatalf("token = %q", res.Token)
	}
}

// TS convertToGoogleInteractionsInput parses a string tool input with
// secureJsonParse, which reads a number beyond the double range as Infinity:
// the input is still the parsed object, not {value: input}.
func TestR97provgInteractionsToolArgsNonFiniteNumber(t *testing.T) {
	args, raw := interactionToolArgs(nil, `{"a":1e400}`)
	f, ok := args["a"].(float64)
	if !ok || !math.IsInf(f, 1) || raw == "" {
		t.Fatalf("args = %#v raw = %q, want {a: +Inf}", args, raw)
	}
	if args, _ := interactionToolArgs(nil, `{"__proto__":{}}`); args["value"] != `{"__proto__":{}}` {
		t.Fatalf("prototype key: args = %#v", args)
	}
}

const r97provgNullTypeError = "Cannot read properties of null (reading 'setupComplete')"

func r97provgDrainErr(next func() error) error {
	for i := 0; i < 100; i++ {
		if err := next(); err != nil {
			return err
		}
	}
	return nil
}

// TS onMessageText reads message.setupComplete of the safeParseJSON value: a
// JSON null message throws a TypeError, which fails the stream
// (cr-r97-provb-to-provg 1).
func TestR97provgLiveTranscriptionNullMessageTypeError(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()
	server.toSend <- nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := newLiveTestModel(t, server.ts.URL).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio: newChanAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	err = r97provgDrainErr(func() error { _, err := result.Stream.Next(); return err })
	if err == nil || err.Error() != r97provgNullTypeError {
		t.Fatalf("err = %v, want the TypeError", err)
	}
}

func TestR97provgSpeechTranslationNullMessageTypeError(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()
	server.toSend <- nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := newSpeechTranslationTestModel(t, server.ts.URL, 0).DoStream(ctx, &provider.SpeechTranslationStreamOptions{
		Audio: newChanAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"}, TargetLanguage: "es",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	err = r97provgDrainErr(func() error { _, err := result.Stream.Next(); return err })
	if err == nil || err.Error() != r97provgNullTypeError {
		t.Fatalf("err = %v, want the TypeError", err)
	}
}

// TS usage.raw is the usage object as sent: a fractional count stays exact in
// raw while the typed fields truncate (cr-r97-provb-to-provg 2).
func TestR97provgInteractionsUsageRawKeepsFraction(t *testing.T) {
	var u interactionsUsage
	if err := json.Unmarshal([]byte(`{"total_input_tokens":1.5,"total_tokens":3}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.raw["total_input_tokens"] != 1.5 {
		t.Fatalf("raw = %#v, want total_input_tokens 1.5", u.raw)
	}
	if u.TotalInputTokens == nil || *u.TotalInputTokens != 1 {
		t.Fatalf("typed = %v", u.TotalInputTokens)
	}
	var wrapped struct {
		Usage *interactionsUsage `json:"usage"`
	}
	if err := providerutils.UnmarshalChunk([]byte(`{"usage":{"total_input_tokens":1.5}}`), &wrapped); err != nil {
		t.Fatal(err)
	}
	if wrapped.Usage.raw["total_input_tokens"] != 1.5 {
		t.Fatalf("chunk raw = %#v", wrapped.Usage.raw)
	}
}

// TS parses a responses-file line with safeParseJSON: one JSONParseError
// whose cause is the SyntaxError (cr-r97-prova-to-provg class).
func TestR97provgBatchLineSingleJSONParseError(t *testing.T) {
	err := googleBatchLineError([]byte(`{"key":"k","__proto__":{}}`))
	var parseErr *providererrors.JSONParseError
	if !errors.As(err, &parseErr) || parseErr.Cause != jsonexact.ErrForbiddenPrototype {
		t.Fatalf("err = %#v, want one JSONParseError caused by the SyntaxError", err)
	}
}

// TS parseEvent reads data.setupComplete of the parsed message: a JSON null
// throws a TypeError, which the realtime event channel treats as fatal
// (cr-r97-provb-to-provg 3).
func TestR97provgRealtimeParseNullEvent(t *testing.T) {
	m := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-live-2.5-flash")
	const want = "Cannot read properties of null (reading 'setupComplete')"
	if _, err := m.ParseServerEvent(json.RawMessage(`null`)); err == nil || err.Error() != want {
		t.Fatalf("ParseServerEvent err = %v", err)
	}
	if _, err := m.NewServerEventParser()(json.RawMessage(`null`)); err == nil || err.Error() != want {
		t.Fatalf("NewServerEventParser err = %v", err)
	}
	if events, err := m.ParseServerEvent(json.RawMessage(`{"setupComplete":{}}`)); err != nil || len(events) != 1 || events[0].Type != "session-created" {
		t.Fatalf("events = %v, %v", events, err)
	}
}
