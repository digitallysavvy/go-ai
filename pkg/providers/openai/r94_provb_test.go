package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r94provbErrorFrame = `{"error":{"code":"insufficient_quota","message":"quota exceeded","type":"insufficient_quota","param":null,"extra":1},"other":2}`

// TestR94provbEarlyStreamErrorDataIsErrorField: TS Chat and Completions
// streams pass chunk.error (the openaiErrorDataSchema output: message, type,
// param, code) to throwIfOpenAIStreamErrorBeforeOutput, so the thrown
// APICallError's data is that inner object and its responseBody is
// JSON.stringify of it, not the whole frame.
func TestR94provbEarlyStreamErrorDataIsErrorField(t *testing.T) {
	const wantBody = `{"message":"quota exceeded","type":"insufficient_quota","param":null,"code":"insufficient_quota"}`
	for name, next := range map[string]func() error{
		"chat": func() error {
			s := newOpenAIStream(io.NopCloser(strings.NewReader("data: "+r94provbErrorFrame+"\n\n")), true)
			defer s.Close() //nolint:errcheck
			_, err := s.Next()
			return err
		},
		"completion": func() error {
			s := newCompletionStream(io.NopCloser(strings.NewReader("data: "+r94provbErrorFrame+"\n\n")), false)
			defer s.Close() //nolint:errcheck
			_, err := s.Next()
			return err
		},
	} {
		var pe *providererrors.ProviderError
		if err := next(); !errors.As(err, &pe) {
			t.Fatalf("%s: err = %T %v", name, err, err)
		}
		if pe.ResponseBody != wantBody {
			t.Errorf("%s: responseBody = %s, want %s", name, pe.ResponseBody, wantBody)
		}
		if got, _ := json.Marshal(pe.Data); string(got) != `{"code":"insufficient_quota","message":"quota exceeded","param":null,"type":"insufficient_quota"}` {
			t.Errorf("%s: data = %s", name, got)
		}
		if pe.StatusCode != 429 || pe.IsRetryable() {
			t.Errorf("%s: status %d retryable %v", name, pe.StatusCode, pe.IsRetryable())
		}
	}
}

// TestR94provbMidStreamErrorDataIsSchemaOutput: after output has started,
// TS createOpenAIProviderStreamError(value.error) gets the parsed chunk, so
// the stream error's data has only the schema keys.
func TestR94provbMidStreamErrorDataIsSchemaOutput(t *testing.T) {
	s := newOpenAIStream(io.NopCloser(strings.NewReader(
		`data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}`+"\n\n"+
			"data: "+r94provbErrorFrame+"\n\n")), true)
	defer s.Close() //nolint:errcheck
	for {
		c, err := s.Next()
		if err != nil {
			t.Fatal("no error part")
		}
		if c.Type != provider.ChunkTypeError {
			continue
		}
		var spe *providererrors.StreamProviderError
		if !errors.As(c.Err, &spe) {
			t.Fatalf("err = %T", c.Err)
		}
		if got, _ := json.Marshal(spe.Data); string(got) != `{"code":"insufficient_quota","message":"quota exceeded","param":null,"type":"insufficient_quota"}` {
			t.Errorf("data = %s", got)
		}
		return
	}
}

// TestR94provbEditResolvesKeyBeforeDownload (cr-r94-tools-to-provb): TS
// resolves config.headers before it downloads URL edit inputs, so a missing
// key or a failing APIKeyFunc is the error even when the download would fail
// (OpenAI image edits and the OpenAI-compatible image edits).
func TestR94provbEditResolvesKeyBeforeDownload(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	edit := &provider.ImageGenerateOptions{Prompt: "x", Files: []provider.ImageFile{{Type: "url", URL: "https://127.0.0.1:1/x.png"}}}
	_, err := NewImageModel(New(Config{}), "gpt-image-1").DoGenerate(context.Background(), edit)
	if !providererrors.IsLoadAPIKeyError(err) {
		t.Fatalf("openai: err = %T %v, want LoadAPIKeyError", err, err)
	}
	keyErr := errors.New("r94provb key")
	calls := 0
	keyFunc := func(context.Context) (string, error) { calls++; return "", keyErr }
	_, err = NewImageModel(New(Config{APIKeyFunc: keyFunc, Name: "acme", BaseURL: "https://example.invalid/v1", OpenAICompatible: true}), "m").DoGenerate(context.Background(), edit)
	if !errors.Is(err, keyErr) || calls != 1 {
		t.Fatalf("compat: err = %T %v (calls %d), want the key function error", err, err, calls)
	}
}

// TestR94provbUnfollowed3xxIsError (cr-r94-provg-to-provb): TS
// postFormDataToApi checks response.ok, so a 300 (no Location, not followed)
// runs the failed-response handler instead of parsing a success.
func TestR94provbUnfollowed3xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMultipleChoices)
		_, _ = w.Write([]byte(`{"id":"file-1","object":"file","bytes":1,"created_at":1,"filename":"a.txt","purpose":"user_data","name":"s","latest_version":"1"}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	check := func(name string, err error) {
		t.Helper()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != http.StatusMultipleChoices {
			t.Errorf("%s: err = %T %v, want the 300 API error", name, err, err)
		}
	}
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}, MediaType: "text/plain", Filename: "a.txt"})
	check("files", err)
	_, err = p.Skills().UploadSkill(context.Background(), types.UploadSkillOptions{Files: []types.UploadSkillFile{{Path: "SKILL.md", Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}}}})
	check("skills", err)
}

type r94provbClosingReader struct{ closed bool }

func (r *r94provbClosingReader) Read([]byte) (int, error) { return 0, io.EOF }
func (r *r94provbClosingReader) Close() error           { r.closed = true; return nil }

// TestR94provbSkillStreamFileUnsupported (cr-r94-prova-to-provb sweep): TS
// uploadSkill converts each file with convertInlineFileDataToUint8Array,
// which cancels a stream and throws UnsupportedFunctionalityError.
func TestR94provbSkillStreamFileUnsupported(t *testing.T) {
	stream := &r94provbClosingReader{}
	_, err := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}).Skills().UploadSkill(context.Background(), types.UploadSkillOptions{
		Files: []types.UploadSkillFile{{Path: "SKILL.md", Data: types.FileData{Type: types.FileDataTypeStream, Stream: stream}}}})
	var ufe *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &ufe) || ufe.Functionality != "streaming file upload" || !stream.closed {
		t.Fatalf("err = %T %v, closed %v", err, err, stream.closed)
	}
}

// TestR94provbFilesUploadOptionEdges: TS `purpose ?? 'assistants'` sends a
// present "" purpose, z.number() rejects a NaN expiresAfter, and the headers
// (API key) are resolved before the file data is converted.
func TestR94provbFilesUploadOptionEdges(t *testing.T) {
	var gotPurpose = "unset"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			if v, ok := r.MultipartForm.Value["purpose"]; ok {
				gotPurpose = v[0]
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-1"}`))
	}))
	defer srv.Close()
	files := New(Config{APIKey: "k", BaseURL: srv.URL}).Files()
	data := types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}
	if _, err := files.UploadFile(context.Background(), types.UploadFileOptions{Data: data, ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"purpose": ""}}}); err != nil {
		t.Fatal(err)
	}
	if gotPurpose != "" {
		t.Errorf("purpose = %q, want \"\"", gotPurpose)
	}
	if _, err := files.UploadFile(context.Background(), types.UploadFileOptions{Data: data, ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"expiresAfter": math.NaN()}}}); err == nil {
		t.Error("NaN expiresAfter accepted")
	}
	t.Setenv("OPENAI_API_KEY", "")
	_, err := New(Config{BaseURL: srv.URL}).Files().UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, DataString: "!!not base64!!"}})
	if !providererrors.IsLoadAPIKeyError(err) {
		t.Errorf("err = %T %v, want LoadAPIKeyError", err, err)
	}
}

// TestR94provbBatchResultLineSchema: TS createJsonLinesResponseHandler
// (openaiBatchResultLineSchema) fails the results stream on a line without a
// string custom_id or with an error lacking a string code / message.
func TestR94provbBatchResultLineSchema(t *testing.T) {
	ok := `{"custom_id":"a","response":null,"error":{"code":"batch_cancelled","message":"x"}}`
	for _, bad := range []string{
		`{"response":null,"error":{"code":"batch_cancelled","message":"x"}}`,
		`{"custom_id":"b","error":{"message":"x"}}`,
		`{"custom_id":"b","response":{"body":{}}}`,
	} {
		mux := http.NewServeMux()
		srv := httptest.NewServer(mux)
		mux.HandleFunc("/batches/batch_1", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"batch_1","status":"completed","output_file_id":"file-out"}`))
		})
		mux.HandleFunc("/files/file-out/content", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(ok + "\n" + bad + "\n"))
		})
		stream, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ExperimentalBatch().DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batch_1"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Next(); err != nil {
			t.Fatalf("first line: %v", err)
		}
		if _, err := stream.Next(); err == nil || err == io.EOF {
			t.Errorf("%s: err = %v, want a validation error", bad, err)
		}
		_ = stream.Close()
		srv.Close()
	}
}

// TestR94provbTranscriptionAbortCause (cr-r94-prova-to-provb sweep): TS
// connectToWebSocket fails the stream with abortSignal.reason.
func TestR94provbTranscriptionAbortCause(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()
	cause := errors.New("r94provb caller cancelled")
	ctx, cancel := context.WithCancelCause(context.Background())
	blocking := &chanTestAudioStream{chunks: make(chan []byte)}
	result, err := newTestTranscriptionModel(server.ts.URL).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio:            blocking,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitForReceived(t, "session.update", time.Second)
	cancel(cause)
	for {
		if _, err = result.Stream.Next(); err != nil {
			break
		}
	}
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want the caller's cause", err)
	}
}

// TestR94provbRealtimeStreamURLsAreWHATWG: TS toWebSocketUrl(config.url(...))
// is new URL: default port left out, host lower-cased.
func TestR94provbRealtimeStreamURLsAreWHATWG(t *testing.T) {
	if got, _ := realtimeTranscriptionWebSocketURL("https://API.Example.com:443/v1"); got != "wss://api.example.com/v1/realtime?intent=transcription" {
		t.Errorf("transcription url = %q", got)
	}
	if got, _ := realtimeSpeechTranslationWebSocketURL("https://API.Example.com:443/v1", "m"); got != "wss://api.example.com/v1/realtime/translations?model=m" {
		t.Errorf("translation url = %q", got)
	}
}
