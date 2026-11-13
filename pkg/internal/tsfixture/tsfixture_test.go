package tsfixture

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestCaseFilesAreCompressed keeps the extracted TS case files small: every
// file in a testdata/tscases directory under pkg/ must be committed as
// *.json.gz (see README.md), never as plain JSON.
func TestCaseFilesAreCompressed(t *testing.T) {
	root := filepath.Join("..", "..") // pkg/
	found := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Base(filepath.Dir(path)) != "tscases" {
			return nil
		}
		if filepath.Base(filepath.Dir(filepath.Dir(path))) != "testdata" {
			return nil
		}
		found++
		if !strings.HasSuffix(path, ".json.gz") {
			t.Errorf("%s: case files must be committed gzipped as *.json.gz", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("no testdata/tscases files found; the walk root is wrong")
	}
}
