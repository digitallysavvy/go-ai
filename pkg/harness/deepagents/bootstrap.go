package deepagents

import (
	"context"
	"fmt"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridges"
)

// BootstrapDir is where the recipe writes its state, relative to the harness
// state directory. Mirrors TS `DEEPAGENTS_BOOTSTRAP_DIR`.
const BootstrapDir = ".harness-bootstrap/deepagents"

const pnpmInstallCommand = "pnpm install --frozen-lockfile --store-dir .pnpm-store"

var (
	bootstrapOnce   sync.Once
	cachedBootstrap *harness.Bootstrap
	bootstrapErr    error
)

// GetBootstrap returns the deepagents bootstrap recipe: the embedded bridge
// files (pkg/harness/bridges, adapter "deepagents") plus `pnpm install`
// (ripgrep ships as an npm dependency of the bridge since TS #23344). The result is cached across calls
// and across every configured harness instance (mirrors TS `getBootstrap`,
// module-scoped `cachedBootstrap`).
func GetBootstrap(ctx context.Context) (*harness.Bootstrap, error) {
	bootstrapOnce.Do(func() {
		files, err := bridges.Files(bridges.DeepAgents)
		if err != nil {
			bootstrapErr = err
			return
		}
		order := []string{"bridge.mjs", "package.json", "pnpm-lock.yaml"}
		bootstrapFiles := make([]harness.BootstrapFile, 0, len(order))
		for _, name := range order {
			content, ok := files[name]
			if !ok {
				bootstrapErr = fmt.Errorf("deepagents: embedded bridge asset %q is missing", name)
				return
			}
			bootstrapFiles = append(bootstrapFiles, harness.BootstrapFile{
				Path:    BootstrapDir + "/" + name,
				Content: string(content),
			})
		}
		cachedBootstrap = &harness.Bootstrap{
			HarnessID:    "deepagents",
			BootstrapDir: BootstrapDir,
			Files:        bootstrapFiles,
			Commands: []harness.BootstrapCommand{
				{Command: pnpmInstallCommand},
			},
		}
	})
	return cachedBootstrap, bootstrapErr
}
