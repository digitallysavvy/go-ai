package xai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR94provbUnfollowed3xxIsError (cr-r94-provg-to-provb): TS
// postToApi / postFormDataToApi check response.ok, so a 300 (no Location, not
// followed) runs the failed-response handler instead of parsing a success.
func TestR94provbUnfollowed3xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMultipleChoices)
		_, _ = w.Write([]byte(`{"id":"file-1","filename":"a.txt","bytes":1,"created_at":1,"text":"hi","data":[{"b64_json":"AA=="}]}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1"})
	check := func(name string, err error) {
		t.Helper()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != http.StatusMultipleChoices {
			t.Errorf("%s: err = %T %v, want the 300 API error", name, err, err)
		}
	}
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}, MediaType: "text/plain", Filename: "a.txt"})
	check("files", err)
	sm, _ := p.SpeechModel("grok-tts")
	_, err = sm.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	check("speech", err)
	_, err = NewImageModel(p, ModelGrokImagineImage).DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	check("image", err)
}

// TestR94provbFilesKeyBeforeData: TS resolves the headers (loadApiKey)
// before convertInlineFileDataToUint8Array, so a missing key wins over bad
// inline data.
func TestR94provbFilesKeyBeforeData(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")
	_, err := New(Config{BaseURL: "http://127.0.0.1:1/v1"}).Files().UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, DataString: "!!not base64!!"}})
	if !providererrors.IsLoadAPIKeyError(err) {
		t.Fatalf("err = %T %v, want LoadAPIKeyError", err, err)
	}
}

// TestR94provbBatchTextEmptyErrorAndImageDownloadErrors: TS
// convertXaiBatchTextResponse fails on `error != null` (a present "" too),
// and the image batch URL download uses xaiFailedResponseHandler, so an HTTP
// failure is the xAI API error with the body's message.
func TestR94provbBatchTextEmptyErrorAndImageDownloadErrors(t *testing.T) {
	empty := ""
	if _, berr := convertXAIBatchTextResponse(xaiBatchTextResponseWire{Error: &empty}); berr == nil || berr.Message != "" {
		t.Fatalf("empty error: berr = %#v, want a failure with message \"\"", berr)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"boom","error":"image store down"}`))
	}))
	defer srv.Close()
	b := NewBatch(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1"}))
	_, err := b.convertImageBatchResponse(context.Background(), xaiImageResponse{Data: []xaiImageData{{URL: srv.URL + "/img.png"}}})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 500 || !strings.Contains(pe.Message, "image store down") {
		t.Fatalf("err = %T %v, want the xAI API error", err, err)
	}
}
