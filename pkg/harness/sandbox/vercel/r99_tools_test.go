package vercel

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fetch gives a 204 / 205 reply a null body: 3.9.0 runCommand and
// getLogsStream throw APIError "No response body", and readFile returns
// null. net/http hands back http.NoBody, which Go read as an empty stream
// (stream_ended_early) or an empty file.
func TestR99toolsNullBodyStatuses(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2/sandboxes/sessions/s/fs/read" {
				w.Header().Set("Content-Type", "application/octet-stream")
			} else {
				w.Header().Set("Content-Type", "application/x-ndjson")
			}
			w.WriteHeader(status)
		}))
		client := NewAPIClient(srv.URL, Credentials{Token: "tok"})
		_, err := client.RunCommandWait(t.Context(), "s", runCommandRequest{Command: "echo"})
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Message != "No response body" || apiErr.StatusCode != status {
			t.Errorf("%d: RunCommandWait err = %v", status, err)
		}
		err = client.GetLogs(t.Context(), "s", "c", func(LogEntry) {})
		if !errors.As(err, &apiErr) || apiErr.Message != "No response body" {
			t.Errorf("%d: GetLogs err = %v", status, err)
		}
		rc, err := client.ReadFile(t.Context(), "s", "/f", "")
		if rc != nil || err != nil {
			t.Errorf("%d: ReadFile = %v, %v; want nil, nil", status, rc, err)
		}
		srv.Close()
	}
}
