package vercel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TS vercel-sandbox-session.ts isFileNotFoundError: an error whose message
// matches /no such file|not found|does not exist|ENOENT/i reads as a
// missing file (null); other errors propagate.
func TestR89toolsVercelReadFileNotFoundErrors(t *testing.T) {
	for msg, want := range map[string]bool{
		"File Not Found":            true,
		"open x: no such file":      true,
		"path does not exist":       true,
		"ENOENT: missing":           true,
		"permission denied":         false,
		"lookup h: no such host":    false,
		"Unexpected response (500)": false,
	} {
		if got := isFileNotFoundError(errors.New(msg)); got != want {
			t.Errorf("isFileNotFoundError(%q) = %v, want %v", msg, got, want)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	s := NewSession(&Sandbox{client: NewAPIClient(srv.URL, Credentials{Token: "tok"}), sessionID: "sess"})
	if data, err := s.ReadBinaryFile(context.Background(), "/x"); err == nil || data != nil {
		t.Fatalf("500 read = %v, %v; want the error", data, err)
	}
}
