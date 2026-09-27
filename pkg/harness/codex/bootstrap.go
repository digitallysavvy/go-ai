package codex

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridges"
)

// BootstrapDir mirrors TS `CODEX_BOOTSTRAP_DIR`.
const BootstrapDir = ".harness-bootstrap/codex"

// GetBootstrap returns the codex bootstrap recipe: the embedded bridge
// assets (WG6) plus the install command. Mirrors TS `getCodexBootstrap`.
func GetBootstrap(_ context.Context) (*harness.Bootstrap, error) {
	files, err := bridges.Files(bridges.Codex)
	if err != nil {
		return nil, err
	}
	names := []string{"package.json", "pnpm-lock.yaml", "bridge.mjs"}
	recipe := &harness.Bootstrap{
		HarnessID:    "codex",
		BootstrapDir: BootstrapDir,
		Commands: []harness.BootstrapCommand{
			{Command: "pnpm install --frozen-lockfile --store-dir .pnpm-store"},
		},
	}
	for _, name := range names {
		content, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("codex: embedded bridge asset %q is missing", name)
		}
		recipe.Files = append(recipe.Files, harness.BootstrapFile{
			Path: BootstrapDir + "/" + name, Content: string(content),
		})
	}
	return recipe, nil
}
