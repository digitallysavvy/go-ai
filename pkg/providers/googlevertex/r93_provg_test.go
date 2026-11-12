package googlevertex

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS googleVertexErrorDataSchema has no `details` key: zod strips the
// details of a Vertex error body whatever their type, so a non-array details
// value still gives error.message, and the error data carries only code,
// message and status (googleFailedResponseHandler's schema, used by Google,
// requires details to be an array and keeps it).
func TestR93provgVertexErrorSchemaIgnoresDetails(t *testing.T) {
	sites := map[string]func([]byte) error{
		"embedding": func(b []byte) error {
			return vertexEmbeddingError("google-vertex", &internalhttp.HTTPStatusError{StatusCode: 400, Body: b})
		},
		"cloud tts": func(b []byte) error { return cloudTTSError(400, b, http.Header{}) },
		"video": func(b []byte) error {
			m := &VideoModel{prov: newR93provgProvider(t)}
			return m.handleError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: b})
		},
		"gemini transcription": func(b []byte) error {
			m := &GeminiTranscriptionModel{}
			return m.handleError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: b})
		},
	}
	for name, site := range sites {
		var pe *providererrors.ProviderError
		err := site([]byte(`{"error":{"code":400,"message":"nope","status":"INVALID_ARGUMENT","details":"x"}}`))
		if !errors.As(err, &pe) || pe.Message != "nope" {
			t.Fatalf("%s: non-array details: %v", name, err)
		}
		err = site([]byte(`{"error":{"code":400,"message":"nope","status":"INVALID_ARGUMENT","details":[{"@type":"t"}]}}`))
		if !errors.As(err, &pe) || pe.Message != "nope" {
			t.Fatalf("%s: %v", name, err)
		}
		b, _ := json.Marshal(pe.Data)
		if string(b) != `{"error":{"code":400,"message":"nope","status":"INVALID_ARGUMENT"}}` {
			t.Fatalf("%s: data = %s", name, b)
		}
	}
}

func newR93provgProvider(t *testing.T) *Provider {
	t.Helper()
	p, err := New(Config{Project: "p", Location: "us-central1", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
