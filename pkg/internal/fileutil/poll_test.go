package fileutil

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type pollStatus struct {
	Status string `json:"status"`
}

func TestPollJSON_DecodesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"succeeded"}`))
	}))
	defer srv.Close()

	opts := TrustedOriginDownloadOptions(srv.URL, srv.Client().Transport)
	var status pollStatus
	if _, err := PollJSON(t.Context(), srv.URL+"/status/abc", opts, &status); err != nil {
		t.Fatalf("PollJSON() error = %v", err)
	}
	if status.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", status.Status)
	}
}

func TestPollJSON_RejectsRedirectToBlockedAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()

	opts := TrustedOriginDownloadOptions(srv.URL, srv.Client().Transport)
	var status pollStatus
	if _, err := PollJSON(t.Context(), srv.URL+"/status/abc", opts, &status); err == nil {
		t.Fatal("expected redirect to a blocked address to be rejected")
	}
}

func TestPollJSON_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	opts := TrustedOriginDownloadOptions(srv.URL, srv.Client().Transport)
	var status pollStatus
	if _, err := PollJSON(t.Context(), srv.URL+"/status/abc", opts, &status); err == nil {
		t.Fatal("expected a decode error for a non-JSON body")
	}
}
