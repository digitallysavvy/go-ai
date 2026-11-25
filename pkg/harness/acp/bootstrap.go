package acp

import (
	"context"
	"fmt"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridges"
)

// bootstrapCache caches one harness instance's bootstrap recipe across calls
// (TS's module-scoped `cachedBootstrap` closure, reproduced per harnessId
// since ACP -- unlike DeepAgents/OpenCode -- is a meta adapter with many
// concurrently configured instances that must NOT share a cache entry).
type bootstrapCache struct {
	once   sync.Once
	result *harness.Bootstrap
	err    error
}

// getBootstrap mirrors TS `createACPBootstrap(...).getBootstrap`.
func (c *bootstrapCache) getBootstrap(ctx context.Context, harnessID string, impl implementation) (*harness.Bootstrap, error) {
	c.once.Do(func() {
		c.result, c.err = buildBootstrap(harnessID, impl)
	})
	return c.result, c.err
}

func bootstrapDir(harnessID string) string { return ".harness-bootstrap/" + harnessID }

// pnpmInstallCommand is the bridge installation command. Mirrors TS
// `PNPM_INSTALL_COMMAND`.
const pnpmInstallCommand = "pnpm install --frozen-lockfile --prod --store-dir .pnpm-store"

// buildBootstrap mirrors TS `createACPBootstrap`'s recipe. A locked source is
// the complete bridge installation and therefore replaces the bridge manifest.
// An install-command source installs the agent outside of pnpm, so the bridge
// manifest only provides the bridge's own runtime dependencies.
func buildBootstrap(harnessID string, impl implementation) (*harness.Bootstrap, error) {
	files, err := bridges.Files(bridges.ACP)
	if err != nil {
		return nil, err
	}
	dir := bootstrapDir(harnessID)
	asset := func(name string) (string, error) {
		content, ok := files[name]
		if !ok {
			return "", fmt.Errorf("acp: embedded bridge asset %q is missing", name)
		}
		return string(content), nil
	}
	bridgeEntry, err := asset("bridge.mjs")
	if err != nil {
		return nil, err
	}
	hostToolMCP, err := asset("host-tool-mcp.mjs")
	if err != nil {
		return nil, err
	}
	bridgeFiles := []harness.BootstrapFile{
		{Path: dir + "/bridge.mjs", Content: bridgeEntry},
		{Path: dir + "/host-tool-mcp.mjs", Content: hostToolMCP},
	}
	src := effectiveSource(impl.Source)
	commands := []harness.BootstrapCommand{{Command: pnpmInstallCommand}}
	if src.Type == SourceNPMLocked {
		bridgeFiles = append(bridgeFiles,
			harness.BootstrapFile{Path: dir + "/package.json", Content: src.PackageJSON},
			harness.BootstrapFile{Path: dir + "/pnpm-lock.yaml", Content: src.PnpmLockYAML},
		)
		if src.PnpmWorkspaceYAML != "" {
			bridgeFiles = append(bridgeFiles, harness.BootstrapFile{Path: dir + "/pnpm-workspace.yaml", Content: src.PnpmWorkspaceYAML})
		}
	} else {
		packageJSON, err := asset("package.json")
		if err != nil {
			return nil, err
		}
		lock, err := asset("pnpm-lock.yaml")
		if err != nil {
			return nil, err
		}
		bridgeFiles = append(bridgeFiles,
			harness.BootstrapFile{Path: dir + "/package.json", Content: packageJSON},
			harness.BootstrapFile{Path: dir + "/pnpm-lock.yaml", Content: lock},
			harness.BootstrapFile{Path: dir + "/install.sh", Content: createInstallScript(src.Command)},
		)
		commands = append(commands, harness.BootstrapCommand{Command: "bash install.sh"})
	}
	return &harness.Bootstrap{HarnessID: harnessID, BootstrapDir: dir, Files: bridgeFiles, Commands: commands}, nil
}
