package gemini

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
)

// TS downloadToolResultFiles fetches with downloadBlob: no timeout and no
// SDK user-agent tag (cr-r99-provc-to-integrator §1).
func TestR99integToolResultDownloadIsDownloadBlob(t *testing.T) {
	opts := toolResultDownloadOptions(123)
	if opts.UserAgent != fileutil.UserAgentNone || opts.Timeout != fileutil.NoTimeout || opts.MaxSize != 123 {
		t.Fatalf("options = UserAgent %v Timeout %v MaxSize %d, want UserAgentNone, NoTimeout, 123", opts.UserAgent, opts.Timeout, opts.MaxSize)
	}
}
