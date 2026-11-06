//go:build unix

package libfx

import (
	"os"
	"path/filepath"
	"testing"
)

// The disk compilation cache holds machine code wazero runs after only a
// CRC32 check, so it is never used from a directory others can write.
func TestCompilationCacheRefusesWritableDirectories(t *testing.T) {
	if wazeroVersion() == "" {
		t.Skip("replaced wazero: no disk compilation cache")
	}
	compileDir := func(versionDir string) string {
		entries, _ := os.ReadDir(versionDir)
		for _, e := range entries {
			if e.IsDir() {
				return e.Name()
			}
		}
		return ""
	}
	use := func(versionDir string) {
		hostsMu.Lock()
		defer hostsMu.Unlock()
		compilationCacheFor(versionDir)
	}

	// Private root: the compile directory is created.
	root := t.TempDir()
	versionDir := filepath.Join(root, Version)
	if err := os.MkdirAll(versionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	use(versionDir)
	if compileDir(versionDir) == "" {
		t.Fatal("private cache root: no compile cache directory")
	}

	// A group- or world-writable root or version directory (a squatted
	// temp dir, a shared GO_AI_FX_CACHE_DIR) keeps the cache in memory.
	for _, loose := range []string{"root", "version"} {
		root := t.TempDir()
		versionDir := filepath.Join(root, Version)
		if err := os.MkdirAll(versionDir, 0o700); err != nil {
			t.Fatal(err)
		}
		target := root
		if loose == "version" {
			target = versionDir
		}
		if err := os.Chmod(target, 0o777); err != nil {
			t.Fatal(err)
		}
		use(versionDir)
		if d := compileDir(versionDir); d != "" {
			t.Fatalf("writable %s: compile cache created at %s", loose, d)
		}
	}

	// A symlinked compile directory is refused too.
	root = t.TempDir()
	versionDir = filepath.Join(root, Version)
	if err := os.MkdirAll(versionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	link := filepath.Join(versionDir, "wazero-"+wazeroVersion()+"-"+goosArch())
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	use(versionDir)
	if entries, _ := os.ReadDir(other); len(entries) != 0 {
		t.Fatal("symlinked compile directory was used")
	}
	if privateDir(link) {
		t.Fatal("privateDir accepted a symlink")
	}
}
