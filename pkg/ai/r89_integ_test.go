package ai

import (
	"context"
	"net/http"
	"testing"
)

// TS createDownload (Transcribe audio and prompt file URLs) sets no timeout;
// Go cut these downloads off after 60 s (the fileutil client timeout). The
// caller's ctx bounds them now (cr-r89-provc-to-gen-integrator).
func TestR89integURLDownloadHasNoTimeout(t *testing.T) {
	rec := &r89provcDeadlineRecorder{}
	restoreTransport, restoreValidator := downloadTransport, downloadURLValidator
	downloadTransport = func() http.RoundTripper { return rec }
	downloadURLValidator = func(string) error { return nil }
	t.Cleanup(func() { downloadTransport, downloadURLValidator = restoreTransport, restoreValidator })

	res, err := CreateURLDownloadWithMetadata(nil)(context.Background(), "https://cdn.example/a.mp3")
	if err != nil || string(res.Data) != "video" {
		t.Fatalf("download = %+v, %v", res, err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.hadDeadline {
		t.Fatal("URL download carried a client timeout deadline; TS sets none")
	}
}
