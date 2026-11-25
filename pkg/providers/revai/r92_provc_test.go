package revai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS AISDKError TranscriptionJobPollingTimedOut carries the submission
// response as its cause, like the submission and job failures.
func TestR92provcPollingTimeoutCarriesTheSubmission(t *testing.T) {
	oldTimeout, oldInterval := pollTimeout, pollInterval
	pollTimeout, pollInterval = 30*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { pollTimeout, pollInterval = oldTimeout, oldInterval })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"j1","status":"in_progress","language":"en"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"j1","status":"in_progress"}`))
	}))
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel("machine")
	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.ErrorCode != "TranscriptionJobPollingTimedOut" {
		t.Fatalf("err = %v", err)
	}
	want := revaiJobResponse{ID: "j1", Status: "in_progress", Language: "en"}
	if !reflect.DeepEqual(perr.Data, want) {
		t.Fatalf("Data = %#v, want the submission %#v", perr.Data, want)
	}
}

// TS revaiFailedResponseHandler: APICallError.data is the revaiErrorDataSchema
// output (undeclared keys stripped) for a matching body, and undefined for
// any other body; the same holds for a failed status poll.
func TestR92provcErrorDataIsTheSchemaOutput(t *testing.T) {
	matching := `{"error":{"message":"bad","code":400,"extra":1},"other":true}`
	want := map[string]interface{}{"error": map[string]interface{}{"message": "bad", "code": float64(400)}}
	var perr *providererrors.ProviderError

	if !errors.As(handleError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(matching)}), &perr) || !reflect.DeepEqual(perr.Data, want) {
		t.Fatalf("HTTP error Data = %#v", perr.Data)
	}
	if !errors.As(handleError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"detail":"x"}`)}), &perr) || perr.Data != nil {
		t.Fatalf("non-matching body Data = %#v, want nil", perr.Data)
	}
	dl := &providererrors.DownloadError{URL: "u", StatusCode: 404, Body: []byte(matching)}
	if !errors.As(handleError(dl), &perr) || !reflect.DeepEqual(perr.Data, want) {
		t.Fatalf("poll error Data = %#v", perr.Data)
	}
}

// TestR92provcForgivingBase64 (r92 provc-04): AudioBase64 decodes like TS
// convertBase64ToUint8Array (ASCII whitespace ignored, padding optional).
func TestR92provcForgivingBase64(t *testing.T) {
	tm := &TranscriptionModel{provider: New(Config{APIKey: "k"}), modelID: "machine"}
	body, _, err := tm.buildMultipartBody(&provider.TranscriptionOptions{AudioBase64: "aG\n k", MimeType: "audio/wav"})
	if err != nil || !strings.Contains(body.String(), "\r\n\r\nhi\r\n") {
		t.Fatalf("err=%v body=%q", err, body)
	}
}
