package xai

import (
	"context"
	"errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS returns b64_json untouched (core decodes it with
// convertBase64ToUint8Array) and decodes base64 audio with
// convertBase64ToUint8Array: atob's forgiving rules, base64url mapped.
func TestR93provbBase64Forgiving(t *testing.T) {
	b64 := "aGVs\nbG8"
	im := NewImageModel(New(Config{APIKey: "k"}), "grok-imagine-image")
	images, _, err := im.responseImages(context.Background(), []xaiImageData{{B64JSON: &b64}})
	if err != nil || len(images) != 1 || string(images[0]) != "hello" {
		t.Fatalf("image: %q, %v", images, err)
	}

	res, err := (&Batch{provider: New(Config{APIKey: "k"})}).convertImageBatchResponse(context.Background(), xaiImageResponse{Data: []xaiImageData{{B64JSON: &b64}}})
	if err != nil || len(res.Images) != 1 || string(res.Images[0]) != "hello" {
		t.Fatalf("batch image: %+v, %v", res, err)
	}

	for in, want := range map[string]string{"aGVs bG8": "hello", "-_8": "\xfb\xff"} {
		got, err := decodeXAITranscriptionAudioBase64(in)
		if err != nil || string(got) != want {
			t.Fatalf("transcription audio %q: %q, %v", in, got, err)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"audio":"aGVs\nbG8","content_type":"audio/mpeg"}`)
	}))
	defer srv.Close()
	sm, err := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel("grok-tts")
	if err != nil {
		t.Fatal(err)
	}
	speech, err := sm.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:            "hi",
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"withTimestamps": true}},
	})
	if err != nil || string(speech.Audio) != "hello" {
		t.Fatalf("speech: %v", err)
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
	r93provbFilesHeaderErrors(t, p.Files(), "xai", sentinel)
	vm, err := p.VideoModel("grok-imagine-video")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := vm.(*VideoModel).checkVideoStatus(context.Background(), "req-1", nil); err != sentinel {
		t.Errorf("checkVideoStatus err = %#v", err)
	}
}

// TS new Error(raw.message ?? 'xAI STT error'): a numeric message is
// String()ed, and still fails the stream.
func TestR93provbTranscriptionStreamNumericErrorMessage(t *testing.T) {
	server := newXAISTTTestServer(t)
	defer server.close()
	model := newTestXAITranscriptionModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newXAIChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	errCh := make(chan error, 1)
	go func() {
		_, err := drainXAIStream(result.Stream)
		errCh <- err
	}()
	server.send(map[string]interface{}{"type": "error", "message": 42.0})
	select {
	case err = <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
	if err == nil || err.Error() != "42" {
		t.Fatalf("err = %v, want 42", err)
	}
}

// TS inputFileExpiresAfter is z.number().optional(): a present null fails.
func TestR93provbBatchExpiresAfterNull(t *testing.T) {
	if _, err := extractXAIBatchProviderOptions(map[string]interface{}{"xai": map[string]interface{}{"inputFileExpiresAfter": nil}}); err == nil {
		t.Fatal("null accepted")
	}
	if _, err := extractXAIBatchProviderOptions(map[string]interface{}{"xai": map[string]interface{}{"inputFileExpiresAfter": 7200}}); err != nil {
		t.Fatal(err)
	}
}
