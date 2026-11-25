package gladia

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonResponseHandler reads response.text() (TextDecoder drops a
// UTF-8 BOM): a BOM-prefixed poll result parses.
func TestR94provcPollBodyIsText(t *testing.T) {
	_, res, err := r91provcRun(t, "\xEF\xBB\xBF"+`{"status":"done","result":{"metadata":{"audio_duration":1},"transcription":{"full_transcript":"hello","languages":["en"],"utterances":[]}}}`, nil)
	if err != nil || res.Text != "hello" {
		t.Fatalf("err = %v, res = %+v", err, res)
	}
	_, _, err = r91provcRun(t, "\xEF\xBB\xBF"+`{"status":"error","error_code":500}`, nil)
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.ErrorCode != "TranscriptionJobFailed" {
		t.Fatalf("err = %T %v, want TranscriptionJobFailed", err, err)
	}
}

// A failed poll body is read as text too (TS gladiaFailedResponseHandler).
func TestR94provcPollErrorBodyIsText(t *testing.T) {
	dl := &providererrors.DownloadError{URL: "u", StatusCode: 404, Body: []byte("\xEF\xBB\xBF" + `{"error":{"message":"bad thing","code":404}}`)}
	var perr *providererrors.ProviderError
	if !errors.As(handleError(dl), &perr) || perr.Message != "bad thing" {
		t.Fatalf("err = %v, want message bad thing", perr)
	}
}

// TS polls with abortSignal.throwIfAborted() (before a poll and after the
// interval), which throws the abort reason: a cancel cause is the error.
func TestR94provcPollAbortCause(t *testing.T) {
	reason := errors.New("r94provc user stop")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/upload":
			_, _ = io.WriteString(w, `{"audio_url":"u"}`)
		case "/v2/pre-recorded":
			_, _ = io.WriteString(w, `{"result_url":"http://`+r.Host+`/v2/job"}`)
		default:
			_, _ = io.WriteString(w, `{"status":"processing"}`)
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancelCause(context.Background())
	time.AfterFunc(50*time.Millisecond, func() { cancel(reason) })
	model, _ := New(Config{APIKey: "k", BaseURL: srv.URL + "/v2"}).TranscriptionModel("default")
	_, err := model.DoTranscribe(ctx, &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"})
	if err != reason {
		t.Fatalf("err = %v, want the cancel cause", err)
	}
}
