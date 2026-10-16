package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadStoreModeFromConfig_JSWhitespace(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// JS trimStart strips U+00A0, so this is a table header and stops the scan.
	write("\u00a0[x]\ncli_auth_credentials_store = \"file\"\n")
	if got := readStoreModeFromConfig(dir); got != "" {
		t.Fatalf("nbsp-indented table header: got %q, want empty", got)
	}
	// JS \s matches U+00A0 around the key.
	write("\u00a0cli_auth_credentials_store\u00a0=\u00a0\"file\"\u00a0# c\n")
	if got := readStoreModeFromConfig(dir); got != "file" {
		t.Fatalf("nbsp-padded key: got %q, want file", got)
	}
}
