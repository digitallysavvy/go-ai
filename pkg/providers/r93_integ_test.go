package providers_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// r93integDownloadBlobPorts are provider files whose fileutil downloads port
// TS downloadBlob / download (a DownloadError on any failure, no
// handleFetchError), or that classify the failure with their own helper.
var r93integDownloadBlobPorts = map[string]string{
	"azure/azure_mai_image_model.go": "edit input files: TS downloadBlob",
	"bfl/image_model.go":             "data: URLs only; HTTP downloads go through bflFetchError",
	"deepinfra/image_model.go":       "edit input files: TS downloadBlob",
	"gemini/downloads.go":            "tool-result files: TS download",
	"openai/image_model.go":          "edit input files and markup: TS downloadBlob",
	"prodia/language_model.go":       "prompt image: TS download",
	"prodia/video_model.go":          "input files: TS downloadBlob",
}

// TestR93integGetFromAPIDownloadsClassifyTransportFailures is the class
// guard for cr-r93-provc-to-{tools,provb,provg}: a provider file that ports
// TS getFromApi through fileutil (Download, DownloadWithMetadata, PollJSON)
// must turn a connection that failed before any response into the retryable
// "Cannot connect to API" error with fileutil.TransportFailure +
// internalhttp.TransportErrorCtx, as TS handleFetchError does. A file that
// ports downloadBlob instead is listed in r93integDownloadBlobPorts.
func TestR93integGetFromAPIDownloadsClassifyTransportFailures(t *testing.T) {
	call := regexp.MustCompile(`fileutil\.(Download|DownloadWithMetadata|PollJSON)\(`)
	var missing []string
	seen := map[string]bool{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !call.Match(src) {
			return nil
		}
		rel := filepath.ToSlash(path)
		if _, ok := r93integDownloadBlobPorts[rel]; ok {
			seen[rel] = true
			return nil
		}
		if !strings.Contains(string(src), "fileutil.TransportFailure(") {
			missing = append(missing, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) > 0 {
		t.Errorf("getFromApi ports without fileutil.TransportFailure (or list them as downloadBlob ports):\n%s", strings.Join(missing, "\n"))
	}
	for f := range r93integDownloadBlobPorts {
		if !seen[f] {
			t.Errorf("stale allowlist entry %s: it has no fileutil download call", f)
		}
	}
}
