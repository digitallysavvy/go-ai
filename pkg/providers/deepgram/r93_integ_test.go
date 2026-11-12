package deepgram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR93integEmptyChannelsFailTheCall: TS maps the segments with
// `response.results?.channels[0].alternatives[0].words`, so a present
// results object with no channel (or a channel with no alternative) throws
// a TypeError and the call fails. Null or absent results give empty text
// and segments (main ruling, round 93: no S3 known difference).
func TestR93integEmptyChannelsFailTheCall(t *testing.T) {
	for _, tc := range []struct {
		body, wantErr string
	}{
		{`{"results":{"channels":[]}}`, "Cannot read properties of undefined (reading 'alternatives')"},
		{`{"results":{"channels":[{"alternatives":[]}]}}`, "Cannot read properties of undefined (reading 'words')"},
		{"\xEF\xBB\xBF" + `{"results":{"channels":[]}}`, "Cannot read properties of undefined (reading 'alternatives')"},
		{`{"results":null}`, ""},
		{`{}`, ""},
		{`{"results":{"channels":[{"alternatives":[{"transcript":"hi","words":[]}]}]}}`, ""},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(tc.body))
		}))
		m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel("nova-2")
		res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
		srv.Close()
		if tc.wantErr != "" {
			if err == nil || err.Error() != tc.wantErr {
				t.Errorf("%s: err = %v, want %q", tc.body, err, tc.wantErr)
			}
			continue
		}
		if err != nil || res == nil {
			t.Errorf("%s: err = %v", tc.body, err)
		}
	}
}
