package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

func r95provbProvider(t *testing.T, h http.HandlerFunc) *Provider {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(Config{APIKey: "k", BaseURL: srv.URL})
}

// TestR95provbFilesFractionalBytesMetadata: openaiFilesResponseSchema
// `bytes` is z.number(), so TS reports a fractional count as is in
// providerMetadata.openai.bytes; ByteSize (an int64 field) is truncated.
func TestR95provbFilesFractionalBytesMetadata(t *testing.T) {
	p := r95provbProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-1","object":"file","bytes":10.5}`))
	})
	res, err := p.Files().(*FilesAPI).GetFileMetadata(context.Background(), provider.FileMetadataOptions{File: types.ProviderReference{"openai": "file-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.ProviderMetadata["openai"].(map[string]interface{})["bytes"]; got != 10.5 {
		t.Errorf("metadata bytes = %#v, want 10.5", got)
	}
	if res.ByteSize == nil || *res.ByteSize != 10 {
		t.Errorf("ByteSize = %v, want 10", res.ByteSize)
	}
}

// TestR95provbStreamErrorCodeAndArrayError: TS parseStreamError reads the
// code with getStringOrNumber (any other value is undefined), and
// asRecord(value.error) is an array error member too, which has no string
// message, so the frame is not a stream error.
func TestR95provbStreamErrorCodeAndArrayError(t *testing.T) {
	se := newOpenAIStreamProviderErrorChunk("openai.chat", []byte(`{"message":"boom","code":true,"type":"server_error"}`))
	if se.Code != nil {
		t.Errorf("boolean code: Code = %#v, want nil", se.Code)
	}
	if frame := parseOpenAIStreamError([]byte(`{"error":[1],"message":"boom","code":"rate_limit"}`)); frame != nil {
		t.Errorf("array error member: frame = %+v, want nil", frame)
	}
	if frame := parseOpenAIStreamError([]byte(`{"type":"response.failed","response":{"error":{"message":"m","code":{"x":1}}}}`)); frame == nil || frame.Code != nil {
		t.Errorf("response.failed object code: frame = %+v, want code nil", frame)
	}
}

// TestR95provbWebSocketOpenFailure (cr-r95-prova-to-provb): TS
// connectToWebSocket fails a socket that does not open (a 401 handshake)
// with the onSocketError text, and an aborted dial with abortSignal.reason.
func TestR95provbWebSocketOpenFailure(t *testing.T) {
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer refused.Close()
	rate := 24000
	open := map[string]func(ctx context.Context) (func() error, error){
		"OpenAI realtime transcription error": func(ctx context.Context) (func() error, error) {
			res, err := newTestTranscriptionModel(refused.URL).DoStream(ctx, &provider.TranscriptionStreamOptions{Audio: newChanTestAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"}})
			if err != nil {
				return nil, err
			}
			return func() error { return r95provbDrainErr(func() error { _, e := res.Stream.Next(); return e }) }, nil
		},
		"OpenAI realtime translation error": func(ctx context.Context) (func() error, error) {
			res, err := newTestSpeechTranslationModel(refused.URL).DoStream(ctx, &provider.SpeechTranslationStreamOptions{Audio: newChanTestAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate}, TargetLanguage: "es"})
			if err != nil {
				return nil, err
			}
			return func() error { return r95provbDrainErr(func() error { _, e := res.Stream.Next(); return e }) }, nil
		},
	}
	for want, start := range open {
		next, err := start(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := next(); err == nil || err.Error() != want {
			t.Errorf("refused handshake: err = %v, want %q", err, want)
		}
		cause := errors.New("r95provb aborted")
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(cause)
		next, err = start(ctx)
		if err == nil {
			err = next()
		}
		if !errors.Is(err, cause) {
			t.Errorf("%s aborted dial: err = %v, want the abort cause", want, err)
		}
	}
}

func r95provbDrainErr(next func() error) error {
	for {
		if err := next(); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// TestR95provbEmptyStopSequencesSent: TS chat sends `stop: stopSequences`,
// so an empty array is sent as `stop: []`.
func TestR95provbEmptyStopSequencesSent(t *testing.T) {
	body, _, err := NewLanguageModel(New(Config{APIKey: "k"}), "gpt-4o").buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, StopSequences: []string{}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if stop, ok := body["stop"].([]string); !ok || len(stop) != 0 {
		t.Fatalf("stop = %#v, want []", body["stop"])
	}
}

// TestR95provbCompletionOutputOnAnyChoice: TS isOpenAICompletionOutputChunk
// checks every choice, so text on a later choice ends the early-error peek
// and a following error frame is an error part, not a failed call.
func TestR95provbCompletionOutputOnAnyChoice(t *testing.T) {
	s := newCompletionStream(io.NopCloser(strings.NewReader(`data: {"id":"c","created":1,"model":"m","choices":[{"text":"","index":0,"finish_reason":null},{"text":"hi","index":1,"finish_reason":null}]}

data: {"error":{"message":"boom","type":"server_error","param":null,"code":null}}

`)), false)
	sawError := false
	for {
		c, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v (want an error part, not a failed call)", err)
		}
		if c.Type == provider.ChunkTypeError {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("no error part")
	}
}

// TestR95provbBatchResultBodySchema: TS convertOpenAIBatchResult validates
// the body with openaiResponsesResponseSchema, so an output_text part
// without its required annotations array is an invalid result.
func TestR95provbBatchResultBodySchema(t *testing.T) {
	_, berr := convertOpenAIBatchResponseBodyBytes([]byte(`{"id":"r","created_at":1,"model":"m","output":[{"type":"message","role":"assistant","id":"m1","content":[{"type":"output_text","text":"hi"}]}]}`))
	if berr == nil || berr.Code != "invalid_response" {
		t.Fatalf("berr = %+v, want invalid_response", berr)
	}
	if _, berr := convertOpenAIBatchResponseBodyBytes([]byte(`{"id":"r","created_at":1,"model":"m","output":[{"type":"message","role":"assistant","id":"m1","content":[{"type":"output_text","text":"hi","annotations":[]}]}]}`)); berr != nil {
		t.Fatalf("valid body: %+v", berr)
	}
}

// TestR95provbTranscriptionBase64Audio: TS getArgs decodes a string audio
// with convertBase64ToUint8Array, so AudioBase64 is the uploaded file.
func TestR95provbTranscriptionBase64Audio(t *testing.T) {
	var got []byte
	p := r95provbProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if f, _, err := r.FormFile("file"); err == nil {
			got, _ = io.ReadAll(f)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hi"}`))
	})
	_, err := NewTranscriptionModel(p, "whisper-1").DoTranscribe(context.Background(), &provider.TranscriptionOptions{AudioBase64: "aGVsbG8=", MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("uploaded file = %q, want hello", got)
	}
}

// TestR95provbResponsesEmptyIncompleteReason: incomplete_details.reason is a
// required string, so a present "" is an unknown reason: TS
// mapOpenAIResponseFinishReason returns 'other' (tool-calls with a function
// call), not 'stop'; response.failed keeps it as the raw reason; the batch
// no-output message is "(<detail>)" for any non-null detail.
func TestR95provbResponsesEmptyIncompleteReason(t *testing.T) {
	if got := mapResponsesFinishReason(&responses.IncompleteDetails{}, false); got != types.FinishReasonOther {
		t.Errorf("finish = %q, want other", got)
	}
	if got := mapResponsesFinishReason(&responses.IncompleteDetails{}, true); got != types.FinishReasonToolCalls {
		t.Errorf("finish with a function call = %q, want tool-calls", got)
	}
	if got := mapResponsesFinishReason(nil, false); got != types.FinishReasonStop {
		t.Errorf("no details = %q, want stop", got)
	}
	_, berr := convertOpenAIBatchResponseBodyBytes([]byte(`{"id":"r","incomplete_details":{"reason":""}}`))
	if berr == nil || berr.Message != "OpenAI Responses returned no output ()." {
		t.Errorf("batch = %+v", berr)
	}
}
