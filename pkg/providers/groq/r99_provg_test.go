package groq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS groqFailedResponseHandler (createJsonErrorResponseHandler) uses
// response.statusText for a body outside the error schema: the status line's
// reason phrase, "" when the server sends none. Go replaced an empty phrase
// with the "HTTP <code>: <body>" cause text.
func TestR99provgOffSchemaErrorKeepsEmptyReasonPhrase(t *testing.T) {
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close() //nolint:errcheck
		_, _ = buf.WriteString("HTTP/1.1 599\r\nContent-Length: 9\r\nConnection: close\r\n\r\nnot json!")
		_ = buf.Flush()
	})
	srv.Start()
	defer srv.Close()

	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "llama-3.3-70b-versatile")
	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("err = %#v, want a ProviderError", err)
	}
	if perr.Message != "" || perr.StatusCode != 599 || perr.ResponseBody != "not json!" {
		t.Fatalf("message %q status %d body %q, want the empty reason phrase", perr.Message, perr.StatusCode, perr.ResponseBody)
	}
}

// TS generate returns `warnings` (an empty array) for both the JSON and the
// text response formats; Go left it nil.
func TestR99provgTranscriptionWarningsEmptyArray(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if format == "text" {
				_, _ = w.Write([]byte("hello"))
				return
			}
			_, _ = w.Write([]byte(`{"text":"hello","x_groq":{"id":"x"}}`))
		}))
		model := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "whisper-large-v3")
		res, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
			Audio: []byte{1, 2}, MimeType: "audio/wav",
			ProviderOptions: map[string]interface{}{"groq": map[string]interface{}{"responseFormat": format}},
		})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.Warnings == nil || len(res.Warnings) != 0 || res.Text != "hello" {
			t.Fatalf("%s: warnings %#v text %q, want an empty array", format, res.Warnings, res.Text)
		}
	}
}
