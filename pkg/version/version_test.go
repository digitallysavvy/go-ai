package version

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/sdkversion"
)

// pkg/telemetry reports pkg/internal/sdkversion in diagnostic messages, so
// the exported Version must be the same constant.
func TestVersionIsTheSDKVersion(t *testing.T) {
	if Version != sdkversion.Version || Version == "" {
		t.Fatalf("Version = %q, sdkversion.Version = %q", Version, sdkversion.Version)
	}
}
