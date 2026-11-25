package revai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS #23679: the job ID is one encoded path segment in the poll and
// transcript URLs.
func TestJobIDEncodedAsSinglePathSegment(t *testing.T) {
	const rawID = "abc/../../internal"
	enc := providerutils.EncodePathSegment(rawID)
	var uris []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.Method+" "+r.RequestURI)
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"abc/../../internal","status":"in_progress"}`))
		case strings.HasSuffix(r.RequestURI, "/transcript"):
			_, _ = w.Write([]byte(`{"monologues":[{"elements":[{"type":"text","value":"hi","ts":0,"end_ts":1}]}]}`))
		default:
			_, _ = w.Write([]byte(`{"id":"abc/../../internal","status":"transcribed"}`))
		}
	}))
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel("machine")
	if _, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /speechtotext/v1/jobs/" + enc, "GET /speechtotext/v1/jobs/" + enc + "/transcript"}
	got := uris[1:]
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("requests = %v, want %v", got, want)
	}
}
