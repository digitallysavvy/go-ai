package codex

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness/bridges"
)

// The client-app version is TS VERSION, the package version of the TS
// release the embedded bridge was built from.
func TestR42ClientAppVersionMatchesBridgeManifest(t *testing.T) {
	m, err := bridges.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if want := m.Adapters["codex"].SourceVersion; version != want {
		t.Fatalf("version = %q, want the bridge manifest's %q", version, want)
	}
}
