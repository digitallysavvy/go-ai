package opencode

import (
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
		order := []string{"package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "bridge.mjs", "host-tool-mcp.mjs"}
		bootstrapFiles := make([]harness.BootstrapFile, 0, len(order))
		for _, name := range order {
			content, ok := files[name]
			if !ok {
				bootstrapErr = fmt.Errorf("opencode: embedded bridge asset %q is missing", name)
				return
			}
			bootstrapFiles = append(bootstrapFiles, harness.BootstrapFile{
				Path:    BootstrapDir + "/" + name,
				Content: string(content),
			})
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
