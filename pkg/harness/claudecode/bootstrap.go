package claudecode

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridges"
)

// BootstrapDir mirrors TS `CLAUDE_CODE_BOOTSTRAP_DIR`.
const BootstrapDir = ".harness-bootstrap/claude-code"

// GetBootstrap returns the claude-code bootstrap recipe: the embedded bridge
// assets (WG6) plus the install/verify commands. Mirrors TS
// `getClaudeCodeBootstrap`.
func GetBootstrap(_ context.Context) (*harness.Bootstrap, error) {
	files, err := bridges.Files(bridges.ClaudeCode)
	if err != nil {
		return nil, err
	}
	names := []string{"package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "bridge.mjs"}
	recipe := &harness.Bootstrap{
		HarnessID:    "claude-code",
		BootstrapDir: BootstrapDir,
		Commands: []harness.BootstrapCommand{
			{Command: "pnpm install --frozen-lockfile --store-dir .pnpm-store"},
			{Command: "./node_modules/.bin/claude --version"},
		},
	}
	for _, name := range names {
		content, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("claudecode: embedded bridge asset %q is missing", name)
		}
		recipe.Files = append(recipe.Files, harness.BootstrapFile{
			Path: BootstrapDir + "/" + name, Content: string(content),
		})
	}
	return recipe, nil
}
