package ai

import (
	"context"
	"strings"
	"testing"
)

func TestCreateDownloadWithNilOptionsRejectsUnsafeURL(t *testing.T) {
	download := CreateDownload(nil)
	if download == nil {
		t.Fatal("CreateDownload(nil) returned nil")
	}

	_, err := download(context.Background(), "http://127.0.0.1/private")
	if err == nil {
		t.Fatal("expected unsafe URL validation error")
	}
	if !strings.Contains(err.Error(), "private network") && !strings.Contains(err.Error(), "blocked") && !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("expected SSRF validation error, got: %v", err)
	}
}

func TestCreateDownloadWithCustomOptionsAndHeadersPath(t *testing.T) {
	download := CreateDownload(&DownloadOptions{
		MaxBytes: 64,
		Headers: map[string]string{
			"X-Test-Header": "value",
		},
	})

	_, err := download(context.Background(), "file:///etc/passwd")
	if err == nil {
		t.Fatal("expected blocked file:// URL")
	}
	if !strings.Contains(err.Error(), "not allowed") && !strings.Contains(err.Error(), "unsupported URL scheme") {
		t.Fatalf("expected scheme validation error, got: %v", err)
	}
}

func TestDefaultDownloadUsesCreateDownloadBehavior(t *testing.T) {
	_, err := DefaultDownload(context.Background(), "http://localhost:8080/file")
	if err == nil {
		t.Fatal("expected localhost to be blocked")
	}
	if !strings.Contains(err.Error(), "localhost") && !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("unexpected error: %v", err)
	}
}
