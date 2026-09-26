package bridges

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"testing"
)

// TestManifestHashesMatchEmbeddedBytes is the WG6 "drift test": it fails if
// anyone hand-edits an embedded bridge asset without re-running the sync tool
// (pkg/harness/bridges/internal/syncbridges) to refresh VERSIONS.json, or
// vice versa. It needs no network access and no TS checkout -- it only
// checks internal consistency of what is committed.
func TestManifestHashesMatchEmbeddedBytes(t *testing.T) {
	m, err := Manifest()
	if err != nil {
		t.Fatalf("Manifest() error: %v", err)
	}
	if m.TSTag == "" || m.TSCommit == "" {
		t.Fatalf("manifest missing tsTag/tsCommit: %+v", m)
	}

	for _, adapter := range Adapters() {
		info, ok := m.Adapters[string(adapter)]
		if !ok {
			t.Errorf("adapter %q has embedded files but no VERSIONS.json entry", adapter)
			continue
		}

		files, err := Files(adapter)
		if err != nil {
			t.Errorf("Files(%q): %v", adapter, err)
			continue
		}

		if len(files) != len(info.Files) {
			t.Errorf("adapter %q: %d embedded files but manifest lists %d", adapter, len(files), len(info.Files))
		}

		for name, data := range files {
			expected, ok := info.Files[name]
			if !ok {
				t.Errorf("adapter %q: embedded file %q is not in VERSIONS.json", adapter, name)
				continue
			}
			sum := sha256.Sum256(data)
			actual := hex.EncodeToString(sum[:])
			if actual != expected {
				t.Errorf("adapter %q file %q: sha256 mismatch\n  manifest: %s\n  actual:   %s\n(the file was edited without re-running syncbridges, or VERSIONS.json is stale)", adapter, name, expected, actual)
			}
		}

		for name := range info.Files {
			if _, ok := files[name]; !ok {
				t.Errorf("adapter %q: VERSIONS.json lists file %q which is not embedded", adapter, name)
			}
		}

		if info.BridgeEntry != "" {
			if _, ok := files[info.BridgeEntry]; !ok {
				t.Errorf("adapter %q: bridgeEntry %q is not among its embedded files", adapter, info.BridgeEntry)
			}
		}
	}

	for name := range m.Adapters {
		found := false
		for _, adapter := range Adapters() {
			if string(adapter) == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("VERSIONS.json has adapter %q which Adapters() does not list", name)
		}
	}
}

// TestEveryEmbeddedFileIsReadable walks the embed.FS and confirms every file
// under an adapter directory is a plain file we can read (catches accidental
// symlinks or directories tsup/pnpm sometimes leave behind).
func TestEveryEmbeddedFileIsReadable(t *testing.T) {
	err := fs.WalkDir(assets, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(assets, path)
		if err != nil {
			t.Errorf("reading %s: %v", path, err)
			return nil
		}
		if len(data) == 0 {
			t.Errorf("%s is empty", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
}

// TestGitHubCopilotAndGrokBuildHaveNoOwnBridgeEntry documents and locks in
// the "thin ACP config" shape: these two adapters ship only an install
// manifest for their target CLI and reuse ACP's bridge.mjs / host-tool-mcp.mjs
// at bootstrap-recipe assembly time (host code, outside this package).
func TestGitHubCopilotAndGrokBuildHaveNoOwnBridgeEntry(t *testing.T) {
	for _, adapter := range []Adapter{GitHubCopilot, GrokBuild} {
		_, ok, err := BridgeEntry(adapter)
		if err != nil {
			t.Fatalf("BridgeEntry(%q): %v", adapter, err)
		}
		if ok {
			t.Errorf("expected %q to have no bridge entry of its own", adapter)
		}
	}
	for _, adapter := range []Adapter{ClaudeCode, Codex, OpenCode, DeepAgents, ACP} {
		data, ok, err := BridgeEntry(adapter)
		if err != nil {
			t.Fatalf("BridgeEntry(%q): %v", adapter, err)
		}
		if !ok || len(data) == 0 {
			t.Errorf("expected %q to have a non-empty bridge entry", adapter)
		}
	}
}
