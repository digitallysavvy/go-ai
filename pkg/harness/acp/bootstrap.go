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

func buildBootstrap(harnessID string, impl implementation) (*harness.Bootstrap, error) {
	files, err := bridges.Files(bridges.ACP)
	if err != nil {
		return nil, err
	}
	dir := bootstrapDir(harnessID)
	bridgeFiles := []harness.BootstrapFile{}
	for _, name := range []string{"package.json", "pnpm-lock.yaml"} {
		content, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("acp: embedded bridge asset %q is missing", name)
		}
		bridgeFiles = append(bridgeFiles, harness.BootstrapFile{Path: dir + "/" + name, Content: string(content)})
	}
	bridgeEntry, ok := files["bridge.mjs"]
	if !ok {
		return nil, fmt.Errorf("acp: embedded bridge asset \"bridge.mjs\" is missing")
	}
	bridgeFiles = append(bridgeFiles, harness.BootstrapFile{Path: dir + "/bridge.mjs", Content: string(bridgeEntry)})
	hostToolMCP, ok := files["host-tool-mcp.mjs"]
	if !ok {
		return nil, fmt.Errorf("acp: embedded bridge asset \"host-tool-mcp.mjs\" is missing")
	}
	bridgeFiles = append(bridgeFiles, harness.BootstrapFile{Path: dir + "/host-tool-mcp.mjs", Content: string(hostToolMCP)})

	bridgeFiles = append(bridgeFiles, harness.BootstrapFile{
		Path: dir + "/implementation/implementation.json", Content: createImplementationDescriptor(impl),
	})
	if manifest := createImplementationManifest(impl); manifest != nil {
		bridgeFiles = append(bridgeFiles, harness.BootstrapFile{Path: dir + "/implementation/package.json", Content: *manifest})
	}
	if lock := getImplementationLockfile(impl); lock != nil {
		bridgeFiles = append(bridgeFiles, harness.BootstrapFile{Path: dir + "/implementation/pnpm-lock.yaml", Content: *lock})
	}
	if workspace := getImplementationWorkspaceFile(impl); workspace != nil {
		bridgeFiles = append(bridgeFiles, harness.BootstrapFile{Path: dir + "/implementation/pnpm-workspace.yaml", Content: *workspace})
	}
	if script := getImplementationInstallScript(impl); script != nil {
		bridgeFiles = append(bridgeFiles, harness.BootstrapFile{Path: dir + "/implementation/install.sh", Content: *script})
	}

	return &harness.Bootstrap{
		HarnessID: harnessID, BootstrapDir: dir, Files: bridgeFiles,
		Commands: []harness.BootstrapCommand{
			{Command: "pnpm install --frozen-lockfile --store-dir .pnpm-store"},
			{Command: createImplementationInstallCommand("implementation", "../.pnpm-store", impl)},
		},
	}, nil
}
