package opencode

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridges"
)

// BootstrapDir is where the recipe writes its state, relative to the harness
// state directory. Mirrors TS `OPENCODE_BOOTSTRAP_DIR`.
const BootstrapDir = ".harness-bootstrap/opencode"

var (
	bootstrapOnce   sync.Once
	cachedBootstrap *harness.Bootstrap
	bootstrapErr    error
)

// readPluginAsset is the OpenCode read plugin the bridge loads in every
// permission mode except allow-all (TS 486e08f044): it overrides the native
// read tool so a nested AGENTS.md symlink cannot bypass the outside-workdir
// read approval.
const readPluginAsset = "opencode-read-plugin.mjs"

// bootstrapFilesFor lists the bootstrap files in TS order: the read plugin
// first, then package.json, the lockfile, the workspace file, the bridge and
// the host-tool MCP server. A bridge that names the read plugin cannot start
// without it (it fails before inference when the plugin did not load), so the
// plugin is required then; a bridge older than the plugin ships without it.
func bootstrapFilesFor(files map[string][]byte) ([]harness.BootstrapFile, error) {
	order := []string{"package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "bridge.mjs", "host-tool-mcp.mjs"}
	if _, ok := files[readPluginAsset]; ok || bytes.Contains(files["bridge.mjs"], []byte(readPluginAsset)) {
		order = append([]string{readPluginAsset}, order...)
	}
	bootstrapFiles := make([]harness.BootstrapFile, 0, len(order))
	for _, name := range order {
		content, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("opencode: embedded bridge asset %q is missing", name)
		}
		bootstrapFiles = append(bootstrapFiles, harness.BootstrapFile{
			Path:    BootstrapDir + "/" + name,
			Content: string(content),
		})
	}
	return bootstrapFiles, nil
}

// GetBootstrap returns the OpenCode bootstrap recipe: the embedded bridge
// files (pkg/harness/bridges, adapter "opencode") plus `pnpm install` and a
// smoke-test `opencode --version` command. The result is cached across calls
// and across every configured harness instance. Mirrors TS `getBootstrap`.
func GetBootstrap(ctx context.Context) (*harness.Bootstrap, error) {
	bootstrapOnce.Do(func() {
		files, err := bridges.Files(bridges.OpenCode)
		if err != nil {
			bootstrapErr = err
			return
		}
		bootstrapFiles, err := bootstrapFilesFor(files)
		if err != nil {
			bootstrapErr = err
			return
		}
		cachedBootstrap = &harness.Bootstrap{
			HarnessID:    "opencode",
			BootstrapDir: BootstrapDir,
			Files:        bootstrapFiles,
			Commands: []harness.BootstrapCommand{
				{Command: "pnpm install --frozen-lockfile --store-dir .pnpm-store"},
				{Command: "./node_modules/.bin/opencode --version"},
			},
		}
	})
	return cachedBootstrap, bootstrapErr
}
