package opencode

import (
	"strings"
	"testing"
)

// TS 486e08f044 opencode-harness.test.ts: the read plugin is a bootstrap
// asset, listed first.
func TestPortToolsBootstrapShipsReadPluginFirst(t *testing.T) {
	files := map[string][]byte{
		"package.json": []byte("{}"), "pnpm-lock.yaml": []byte("l"), "pnpm-workspace.yaml": []byte("w"),
		"bridge.mjs": []byte("// bridge"), "host-tool-mcp.mjs": []byte("// mcp"),
		"opencode-read-plugin.mjs": []byte("// plugin"),
	}
	got, err := bootstrapFilesFor(files)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range got {
		paths = append(paths, f.Path)
	}
	want := strings.Join([]string{
		BootstrapDir + "/opencode-read-plugin.mjs",
		BootstrapDir + "/package.json",
		BootstrapDir + "/pnpm-lock.yaml",
		BootstrapDir + "/pnpm-workspace.yaml",
		BootstrapDir + "/bridge.mjs",
		BootstrapDir + "/host-tool-mcp.mjs",
	}, ",")
	if strings.Join(paths, ",") != want {
		t.Fatalf("paths = %v", paths)
	}
	if got[0].Content != "// plugin" {
		t.Fatalf("plugin content = %q", got[0].Content)
	}
}

// A bridge that loads the read plugin cannot run without it, so a missing
// plugin asset fails the bootstrap instead of shipping a bridge that would
// fail before inference. A bridge older than the plugin still bootstraps.
func TestPortToolsBootstrapRequiresReadPluginForBridgeThatNamesIt(t *testing.T) {
	base := map[string][]byte{
		"package.json": []byte("{}"), "pnpm-lock.yaml": []byte("l"), "pnpm-workspace.yaml": []byte("w"),
		"host-tool-mcp.mjs": []byte("// mcp"),
	}
	base["bridge.mjs"] = []byte(`config.plugin = ["opencode-read-plugin.mjs"]`)
	if _, err := bootstrapFilesFor(base); err == nil || !strings.Contains(err.Error(), "opencode-read-plugin.mjs") {
		t.Fatalf("err = %v, want a missing-asset error", err)
	}
	base["bridge.mjs"] = []byte("// old bridge")
	files, err := bootstrapFilesFor(base)
	if err != nil || len(files) != 5 {
		t.Fatalf("files=%d err=%v", len(files), err)
	}
}
