package assemblyai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS: `Transcription failed: ${transcript.error ?? 'Unknown error'}` keeps an
// empty-string error and only substitutes for null/absent.
func TestTranscriptErrorFallbackOnlyForNull(t *testing.T) {
	for errVal, want := range map[string]string{
		`""`:   "Transcription failed: ",
		`null`: "Transcription failed: Unknown error",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/upload":
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"upload_url": "u"})
			case "/transcript":
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "t", "status": "queued"})
			default:
				_, _ = w.Write([]byte(`{"id":"t","status":"error","error":` + errVal + `}`))
			}
		}))
		p := New(Config{APIKey: "k", BaseURL: srv.URL})
		m, _ := p.TranscriptionModel(ModelUniversal35Pro)
		_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a")})
		srv.Close()
		if err == nil || err.Error() != want {
			t.Errorf("error=%s: got %v, want %q", errVal, err, want)
		}
	}
}
