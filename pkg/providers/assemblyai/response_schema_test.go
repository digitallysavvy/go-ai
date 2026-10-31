package assemblyai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS: the upload and submit bodies go through createJsonResponseHandler
// ("Invalid JSON response"); the polled transcript is parsed with a bare
// schema.parse, so a mismatch is the validation error itself.
func TestTranscriptionResponseSchemas(t *testing.T) {
	run := func(upload, submit, transcript string) error {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/upload"):
				_, _ = w.Write([]byte(upload))
			case strings.HasSuffix(r.URL.Path, "/transcript"):
				_, _ = w.Write([]byte(submit))
			default:
				_, _ = w.Write([]byte(transcript))
			}
		}))
		defer srv.Close()
		m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel("universal-2")
		_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a")})
		return err
	}
	okUpload, okSubmit := `{"upload_url":"u"}`, `{"id":"t1","status":"queued"}`
	if err := run(`{}`, okSubmit, `{}`); !providerutils.IsInvalidJSONResponse(err) {
		t.Errorf("upload: %v", err)
	}
	if err := run(okUpload, `{"id":"t1","status":"waiting"}`, `{}`); !providerutils.IsInvalidJSONResponse(err) {
		t.Errorf("submit: %v", err)
	}
	err := run(okUpload, okSubmit, `{"id":"t1","status":"completed","words":[{"text":"a"}]}`)
	var ve *providererrors.ValidationError
	if !errors.As(err, &ve) || providerutils.IsInvalidJSONResponse(err) {
		t.Errorf("transcript: error = %v, want the validation error itself", err)
	}
	if err := run(okUpload, okSubmit, `{"id":"t1","status":"completed","text":"hi","words":[{"text":"a","start":0,"end":10}]}`); err != nil {
		t.Errorf("valid transcript rejected: %v", err)
	}
}
