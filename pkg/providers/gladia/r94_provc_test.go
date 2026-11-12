package gladia

import (
	"errors"
	"testing"

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
