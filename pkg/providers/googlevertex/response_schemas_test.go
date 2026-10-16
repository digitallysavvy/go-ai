package googlevertex

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonResponseHandler(schema): a 2xx body that does not match the
// zod schema is an APICallError "Invalid JSON response" caused by the
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

func serveVertexBody(t *testing.T, body string) *Provider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	p, err := New(Config{Project: "p", Location: "us-central1", AccessToken: "t", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestVertexResponseSchema_Embedding(t *testing.T) {
	ctx := context.Background()
	// statistics.token_count is required by googleVertexTextEmbeddingResponseSchema.
	m := NewEmbeddingModel(serveVertexBody(t, `{"predictions":[{"embeddings":{"values":[1]}}]}`), "text-embedding-005")
	_, err := m.DoEmbedMany(ctx, []string{"a"}, nil)
	assertInvalidJSONResponse(t, err)

	m = NewEmbeddingModel(serveVertexBody(t, `{"embedding":{"values":["x"]}}`), EmbeddingModelGeminiEmbedding2)
	_, err = m.DoEmbedMany(ctx, []string{"a"}, nil)
	assertInvalidJSONResponse(t, err)

	m = NewEmbeddingModel(serveVertexBody(t, `{"predictions":[{"embeddings":{"values":[1],"statistics":{"token_count":2},"x":1}}],"y":1}`), "text-embedding-005")
	if _, err = m.DoEmbedMany(ctx, []string{"a"}, nil); err != nil {
		t.Fatalf("unknown keys must pass: %v", err)
	}
}

func TestVertexResponseSchema_Video(t *testing.T) {
	ctx := context.Background()
	m := NewVideoModel(serveVertexBody(t, `{"name":5}`), "veo-3.1-generate-preview")
	_, err := m.DoStart(ctx, &provider.VideoModelV3StartOptions{})
	assertInvalidJSONResponse(t, err)
	m = NewVideoModel(serveVertexBody(t, `{"done":true,"response":{"videos":[{"gcsUri":1}]}}`), "veo-3.1-generate-preview")
	_, err = m.DoStatus(ctx, &provider.VideoModelV3StatusOptions{Operation: []byte(`{"operationName":"operations/x"}`)})
	assertInvalidJSONResponse(t, err)
}

func TestVertexResponseSchema_GeminiTranscription(t *testing.T) {
	m := NewGeminiTranscriptionModel(serveVertexBody(t, `{"candidates":[{"content":{"parts":[{"text":5}]}}]}`), ModelGemini35Transcribe)
	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	assertInvalidJSONResponse(t, err)
}

// Cloud Speech-to-Text and Cloud TTS bodies use the same schema check.
func TestVertexResponseSchema_ChirpSchemas(t *testing.T) {
	for name, tc := range map[string]struct {
		check func(int, http.Header, []byte) error
		body  string
		bad   bool
	}{
		"stt results not array": {vertexValidator("p", zVertexTranscription), `{"results":{}}`, true},
		"stt transcript number": {vertexValidator("p", zVertexTranscription), `{"results":[{"alternatives":[{"transcript":1}]}]}`, true},
		"stt ok":                {vertexValidator("p", zVertexTranscription), `{"results":[{"alternatives":[{"transcript":"a","x":1}]}],"y":1}`, false},
		"tts audio number":      {vertexValidator("p", zVertexCloudTTS), `{"audioContent":5}`, true},
		"tts ok":                {vertexValidator("p", zVertexCloudTTS), `{}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.check(200, http.Header{}, []byte(tc.body))
			if (err != nil) != tc.bad {
				t.Fatalf("err = %v, bad = %v", err, tc.bad)
			}
		})
	}
}
