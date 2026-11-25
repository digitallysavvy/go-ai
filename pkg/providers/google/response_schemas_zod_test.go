package google

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS createJsonResponseHandler(schema): a 2xx body that does not match the
// zod schema is an APICallError "Invalid JSON response" whose cause is the
// TypeValidationError, status and body preserved.
func assertInvalidJSONResponse(t *testing.T, err error) {
	t.Helper()
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "Invalid JSON response" {
		t.Fatalf("err = %#v, want Invalid JSON response", err)
	}
	if perr.StatusCode != 200 || perr.ResponseBody == "" || !providererrors.IsValidationError(perr.Cause) {
		t.Fatalf("status=%d body=%q cause=%#v", perr.StatusCode, perr.ResponseBody, perr.Cause)
	}
}

func serveBody(t *testing.T, body string) *Provider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return New(Config{APIKey: "k", BaseURL: srv.URL})
}

func TestResponseSchema_Embeddings(t *testing.T) {
	ctx := context.Background()
	_, err := NewEmbeddingModel(serveBody(t, `{"embedding":{"values":"x"}}`), EmbeddingModelGeminiEmbedding001).DoEmbed(ctx, "a", nil)
	assertInvalidJSONResponse(t, err)
	_, err = NewEmbeddingModel(serveBody(t, `{"embeddings":[{"values":[1]},{}]}`), EmbeddingModelGeminiEmbedding001).DoEmbedMany(ctx, []string{"a", "b"}, nil)
	assertInvalidJSONResponse(t, err)
	_, err = NewEmbeddingModel(serveBody(t, `{"embeddings":[{"values":[1]},{"values":[2]}],"extra":true}`), EmbeddingModelGeminiEmbedding001).DoEmbedMany(ctx, []string{"a", "b"}, nil)
	if err != nil {
		t.Fatalf("unknown keys must pass: %v", err)
	}
}

func TestResponseSchema_Video(t *testing.T) {
	ctx := context.Background()
	m := NewVideoModel(serveBody(t, `{"name":5}`), "veo-3.1-generate-preview")
	_, err := m.DoStart(ctx, &provider.VideoModelV3StartOptions{})
	assertInvalidJSONResponse(t, err)
	m = NewVideoModel(serveBody(t, `{"done":"yes"}`), "veo-3.1-generate-preview")
	_, err = m.DoStatus(ctx, &provider.VideoModelV3StatusOptions{Operation: []byte(`{"operationName":"operations/x"}`)})
	assertInvalidJSONResponse(t, err)
	m = NewVideoModel(serveBody(t, `{"done":true,"error":{"code":1}}`), "veo-3.1-generate-preview")
	_, err = m.DoStatus(ctx, &provider.VideoModelV3StatusOptions{Operation: []byte(`{"operationName":"operations/x"}`)})
	assertInvalidJSONResponse(t, err) // error.message is required
}

func TestResponseSchema_Speech(t *testing.T) {
	m := NewSpeechModel(serveBody(t, `{"candidates":[{"content":{"parts":[{"inlineData":{"data":5}}]}}]}`), ModelGemini25FlashTTS)
	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	assertInvalidJSONResponse(t, err)
}

func TestResponseSchema_Transcription(t *testing.T) {
	p := serveBody(t, `{"steps":[{"content":"x"}]}`)
	m, err := p.TranscriptionModel(ModelGemini35Transcribe)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	assertInvalidJSONResponse(t, err)
}

func TestResponseSchema_Interactions(t *testing.T) {
	for name, body := range map[string]string{
		"bad status":      `{"status":"bogus"}`,
		"missing status":  `{"id":"i"}`,
		"steps not array": `{"status":"completed","steps":{}}`,
		"step no type":    `{"status":"completed","steps":[{}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			m := NewInteractionsLanguageModel(serveBody(t, body), ModelGemini25Flash)
			_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			assertInvalidJSONResponse(t, err)
		})
	}
}

func TestResponseSchema_Batch(t *testing.T) {
	ctx := context.Background()
	b := serveBody(t, `{"operations":[{"done":true}]}`).ExperimentalBatch().(provider.BatchV4Lister)
	_, err := b.DoListBatches(ctx, provider.BatchV4ListOptions{})
	assertInvalidJSONResponse(t, err)

	c := serveBody(t, `[]`).ExperimentalBatch().(provider.BatchV4Canceller)
	_, err = c.DoCancelBatch(ctx, provider.BatchV4OperationOptions{BatchID: "batches/b"})
	assertInvalidJSONResponse(t, err)
}

func TestResponseSchema_FilePoll(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/v1beta/files":
			w.Header().Set("x-goog-upload-url", srv.URL+"/resume")
		case "/resume":
			_, _ = w.Write([]byte(`{"file":{"name":"files/a","mimeType":"text/plain","uri":"u","state":"PROCESSING"}}`))
		default:
			_, _ = w.Write([]byte(`{"name":"files/a","state":"ACTIVE","uri":"u"}`)) // mimeType missing
		}
	}))
	defer srv.Close()
	_, err := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"}).Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data: types.FileData{Type: types.FileDataTypeData, Data: []byte{1}}, MediaType: "text/plain",
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"pollIntervalMs": 1}},
	})
	assertInvalidJSONResponse(t, err)
}

// A 2xx body that is not JSON is also createJsonResponseHandler's "Invalid
// JSON response" (cause: JSONParseError), for the language model too.
func TestResponseSchema_NonJSONBodyIsInvalidJSONResponse(t *testing.T) {
	p := serveBody(t, `<html>gateway</html>`)
	model, err := p.LanguageModel(ModelGemini20Flash)
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "Invalid JSON response" || perr.StatusCode != 200 || perr.ResponseBody != `<html>gateway</html>` {
		t.Fatalf("err = %#v", err)
	}
	_, err = NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001).DoEmbed(context.Background(), "a", nil)
	if !errors.As(err, &perr) || perr.Message != "Invalid JSON response" {
		t.Fatalf("embedding err = %#v", err)
	}
}
