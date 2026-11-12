package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The Completions API logprobs object as OpenAI returns it, text_offset
// included.
const r93provbCompletionLogprobs = `{"tokens":[" ever"],"token_logprobs":[-0.06],"top_logprobs":[{" ever":-0.06}],"text_offset":[5]}`

// TS openaiCompletionResponseSchema / openaiCompletionChunkSchema: the
// logprobs zod object keeps tokens, token_logprobs and top_logprobs, so
// providerMetadata.openai.logprobs has no text_offset.
func TestR93provbCompletionLogprobsSchemaKeys(t *testing.T) {
	const want = `{"tokens":[" ever"],"token_logprobs":[-0.06],"top_logprobs":[{" ever":-0.06}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, `data: {"id":"c1","created":1711363440,"model":"m","choices":[{"text":"Hi","index":0,"logprobs":`+r93provbCompletionLogprobs+`,"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c1","created":1711363440,"model":"m","choices":[{"text":"Hi","index":0,"logprobs":`+r93provbCompletionLogprobs+`,"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).CompletionModel("gpt-3.5-turbo-instruct")
	if err != nil {
		t.Fatal(err)
	}
	opts := &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "Hello"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"logprobs": 1}},
	}

	res, err := m.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(res.ProviderMetadata["openai"].(map[string]interface{})["logprobs"])
	if string(got) != want {
		t.Errorf("generate logprobs = %s, want %s", got, want)
	}

	s, err := m.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close() //nolint:errcheck
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil {
		t.Fatal("no finish part")
	}
	var meta struct {
		OpenAI struct {
			Logprobs json.RawMessage `json:"logprobs"`
		} `json:"openai"`
	}
	if err := json.Unmarshal(finish.ProviderMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	if string(meta.OpenAI.Logprobs) != want {
		t.Errorf("stream logprobs = %s, want %s", meta.OpenAI.Logprobs, want)
	}
}

func TestR93provbCompletionLogprobsNullTopLogprobs(t *testing.T) {
	for in, want := range map[string]string{
		`{"tokens":["a"],"token_logprobs":[-1],"top_logprobs":null,"text_offset":[0]}`: `{"tokens":["a"],"token_logprobs":[-1],"top_logprobs":null}`,
		`{"tokens":["a"],"token_logprobs":[-1]}`:                                       `{"tokens":["a"],"token_logprobs":[-1]}`,
	} {
		if got := string(completionLogprobs(json.RawMessage(in))); got != want {
			t.Errorf("completionLogprobs(%s) = %s, want %s", in, got, want)
		}
	}
}

// TS returns b64_json untouched and core decodes it with
// convertBase64ToUint8Array (atob's forgiving rules): a line-wrapped or
// unpadded image decodes. Native and OpenAI-compatible image models.
func TestR93provbImageBase64Forgiving(t *testing.T) {
	for _, compat := range []bool{false, true} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"created":1,"data":[{"b64_json":"aGVs\nbG8"}]}`)
		}))
		m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: compat}), "dall-e-3")
		res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
		srv.Close()
		if err != nil {
			t.Fatalf("compat=%v: %v", compat, err)
		}
		if len(res.Images) != 1 || string(res.Images[0]) != "hello" {
			t.Fatalf("compat=%v: images = %q", compat, res.Images)
		}
	}
}

// TS openaiCompatibleImageResponseSchema b64_json is nullish: a present ""
// is image data (b64_json != null), not a URL-only item.
func TestR93provbCompatImageEmptyBase64(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":""}]}`)
	}))
	defer srv.Close()
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true}), "img")
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Base64Images) != 1 || res.Base64Images[0] != "" {
		t.Fatalf("base64 images = %q", res.Base64Images)
	}
}

// Speech translation audio deltas decode with atob's forgiving rules (TS
// passes the base64 delta on as the audio part).
func TestR93provbSpeechTranslationAudioForgiving(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()
	model := newTestSpeechTranslationModel(server.ts.URL)
	rate := 24000
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanTestAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitForReceived(t, "session.update", time.Second)
	server.toSend <- map[string]interface{}{"type": "session.output_audio.delta", "delta": "aGVs bG8"}
	server.toSend <- map[string]interface{}{"type": "session.closed"}
	parts, _ := drainUntilTranslationFinishOrError(t, result.Stream)
	var audio []string
	for _, p := range parts {
		if p.Type == provider.SpeechTranslationStreamPartTypeAudio {
			audio = append(audio, string(p.AudioData))
		}
	}
	if len(audio) != 1 || audio[0] != "hello" {
		t.Fatalf("audio parts = %q, want [hello]", audio)
	}
}

// TS resolve(this.config.headers) throws the API key callback's error as is:
// the internal FetchRejection marker must not reach the caller
// (cr-r93-prova-to-provb).
func r93provbFilesHeaderErrors(t *testing.T, files provider.FilesAPI, key string, sentinel error) {
	t.Helper()
	ref := types.ProviderReference{key: "file-1"}
	_, err := files.UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}, MediaType: "image/png", Filename: "a.png"})
	if err != sentinel {
		t.Errorf("UploadFile err = %#v, want the sentinel", err)
	}
	if mf, ok := files.(interface {
		GetFileMetadata(context.Context, provider.FileMetadataOptions) (*provider.FileMetadataResult, error)
		DownloadFile(context.Context, provider.DownloadFileOptions) (*provider.DownloadFileResult, error)
		DeleteFile(context.Context, provider.DeleteFileOptions) (*provider.DeleteFileResult, error)
	}); ok {
		if _, err := mf.GetFileMetadata(context.Background(), provider.FileMetadataOptions{File: ref}); err != sentinel {
			t.Errorf("GetFileMetadata err = %#v", err)
		}
		if _, err := mf.DownloadFile(context.Background(), provider.DownloadFileOptions{File: ref}); err != sentinel {
			t.Errorf("DownloadFile err = %#v", err)
		}
		if _, err := mf.DeleteFile(context.Background(), provider.DeleteFileOptions{File: ref}); err != sentinel {
			t.Errorf("DeleteFile err = %#v", err)
		}
	}
}

func TestR93provbHeaderErrorAsIs(t *testing.T) {
	sentinel := errors.New("no key")
	p := New(Config{APIKeyFunc: func(context.Context) (string, error) { return "", sentinel }, BaseURL: "http://127.0.0.1:1"})
	r93provbFilesHeaderErrors(t, p.Files(), "openai", sentinel)
	_, err := p.Skills().UploadSkill(context.Background(), types.UploadSkillOptions{Files: []types.UploadSkillFile{{Path: "s/SKILL.md", Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}}}})
	if err != sentinel {
		t.Errorf("UploadSkill err = %#v", err)
	}
	rm, err := p.RealtimeModel("gpt-realtime")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rm.(*OpenAIRealtimeModel).DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{}); err != sentinel {
		t.Errorf("DoCreateClientSecret err = %#v", err)
	}
}

// TS openaiResponsesResponseSchema error: {message, type, param?, code}
// strings; anything else fails the body ("invalid Responses batch result").
func TestR93provbBatchResultErrorSchema(t *testing.T) {
	for body, want := range map[string]string{
		`{"error":{"message":"m","type":"t","code":"c"}}`:              "m|t|c",
		`{"error":{"message":"m","type":"t","code":7}}`:                "OpenAI returned an invalid Responses batch result.|",
		`{"error":{"message":"m","code":"c"}}`:                         "OpenAI returned an invalid Responses batch result.|",
		`{"error":{"message":"m","type":"t","code":"c","param":1}}`:    "OpenAI returned an invalid Responses batch result.|",
		`{"error":{"message":"m","type":"t","code":"c","param":null}}`: "m|t|c",
	} {
		_, be := convertOpenAIBatchResponseBodyBytes(json.RawMessage(body))
		if be == nil {
			t.Fatalf("%s: no error", body)
		}
		got := be.Message + "|" + be.Type
		if be.Type != "" || be.Code == "c" {
			got += "|" + be.Code
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", body, got, want)
		}
	}
}

// TS new Error(raw.error?.message ?? 'OpenAI realtime error'): a non-string
// message is String()ed (transcription and speech translation).
func TestR93provbRealtimeNumericErrorMessage(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()
	model := newTestTranscriptionModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanTestAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitForReceived(t, "session.update", time.Second)
	server.toSend <- map[string]interface{}{"type": "error", "error": map[string]interface{}{"message": 42.0}}
	for {
		if _, err = result.Stream.Next(); err != nil {
			break
		}
	}
	if err.Error() != "42" {
		t.Fatalf("transcription err = %v, want 42", err)
	}

	server2 := newRealtimeTranscriptionTestServer(t)
	defer server2.close()
	rate := 24000
	tr, err := newTestSpeechTranslationModel(server2.ts.URL).DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanTestAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Stream.Close() //nolint:errcheck
	server2.waitForReceived(t, "session.update", time.Second)
	server2.toSend <- map[string]interface{}{"type": "error", "error": map[string]interface{}{"message": true}}
	server2.toSend <- map[string]interface{}{"type": "session.closed"}
	parts, _ := drainUntilTranslationFinishOrError(t, tr.Stream)
	var msgs []string
	for _, p := range parts {
		if p.Type == provider.SpeechTranslationStreamPartTypeError {
			msgs = append(msgs, p.Err.(error).Error())
		}
	}
	if len(msgs) != 1 || msgs[0] != "true" {
		t.Fatalf("translation error parts = %q, want [true]", msgs)
	}
}

// TS fileToBlob sends every url file through downloadBlob: an empty URL
// fails the download instead of uploading empty bytes (cr-r93-tools-to-provb;
// native and OpenAI-compatible edits).
func TestR93provbImageEditEmptyURLIsDownloaded(t *testing.T) {
	for _, compat := range []bool{false, true} {
		var calls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
		m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: compat}), "gpt-image-1")
		_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", Files: []provider.ImageFile{{Type: "url", URL: ""}}})
		srv.Close()
		var de *providererrors.DownloadError
		if !errors.As(err, &de) || calls != 0 {
			t.Fatalf("compat=%v: err = %#v, calls = %d, want DownloadError and no request", compat, err, calls)
		}
	}
}

// TS inputFileExpiresAfter is z.number().optional(): a present null fails.
func TestR93provbBatchExpiresAfterNull(t *testing.T) {
	if _, err := openaiBatchInputFileExpiresAfter("openai", map[string]interface{}{"openai": map[string]interface{}{"inputFileExpiresAfter": nil}}); err == nil {
		t.Fatal("null accepted")
	}
	if n, err := openaiBatchInputFileExpiresAfter("openai", map[string]interface{}{"openai": map[string]interface{}{}}); err != nil || n != openaiBatchInputFileDefaultExpiresAfterSeconds {
		t.Fatalf("default: %d, %v", n, err)
	}
}

// JS String(number) text for a numeric stream error code.
func TestR93provbStreamErrorCodeNumberText(t *testing.T) {
	if got := openAIStreamErrorCodeString(1e21); got != "1e+21" {
		t.Fatalf("code = %q", got)
	}
}

// TS speech: z.number() rejects a NaN speed, and response.body is the raw
// audio (createBinaryResponseHandler rawValue).
func TestR93provbSpeechNaNAndBody(t *testing.T) {
	if err := validateSpeechProviderOptions(map[string]interface{}{"openai": map[string]interface{}{"speed": math.NaN()}}); err == nil {
		t.Fatal("NaN speed accepted")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("audio"))
	}))
	defer srv.Close()
	res, err := NewSpeechModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "tts-1").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := res.Response.Body.([]byte); string(b) != "audio" {
		t.Fatalf("response body = %#v", res.Response.Body)
	}
}

// TS mediaTypeToExtension: the lower-cased second segment ("a/b/c" -> "b").
func TestR93provbMediaTypeExtension(t *testing.T) {
	for in, want := range map[string]string{"audio/X-WAV": "wav", "audio/OGG": "ogg", "audio/webm/x": "webm", "": "", "audio": ""} {
		if got := getExtensionFromMimeType(in); got != want {
			t.Errorf("getExtensionFromMimeType(%q) = %q, want %q", in, got, want)
		}
	}
}
