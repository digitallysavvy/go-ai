package deepagents

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridges"
)

// BootstrapDir is where the recipe writes its state, relative to the harness
// state directory. Mirrors TS `DEEPAGENTS_BOOTSTRAP_DIR`.
const BootstrapDir = ".harness-bootstrap/deepagents"

// Pinned ripgrep release and per-architecture tarball checksums, verified
// before installation. Mirrors TS deepagents-bootstrap.ts.
const (
	ripgrepVersion     = "14.1.1"
	ripgrepSHA256X64   = "4cf9f2741e6c465ffdb7c26f38056a59e2a2544b51f7cc128ef28337eeae4d8e"
	ripgrepSHA256ARM   = "c827481c4ff4ea10c9dc7a4022c8de5db34a5737cb74484d62eb94a95841ab2f"
	pnpmInstallCommand = "pnpm install --frozen-lockfile --store-dir .pnpm-store"
)

var (
	bootstrapOnce   sync.Once
	cachedBootstrap *harness.Bootstrap
	bootstrapErr    error
)

// GetBootstrap returns the deepagents bootstrap recipe: the embedded bridge
// files (pkg/harness/bridges, adapter "deepagents") plus a ripgrep
// installation command and `pnpm install`. The result is cached across calls
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
				{Command: installRipgrepCommand()},
				{Command: pnpmInstallCommand},
			},
		}
	})
	return cachedBootstrap, bootstrapErr
}

// installRipgrepCommand mirrors TS `installRipgrepCommand`. DeepAgents' grep
// shells out to `rg`; without it the fallback reads the entire workdir
// (including node_modules) into memory and can run out of memory. The
// checksum-verified installation is skipped when `rg` already exists.
func installRipgrepCommand() string {
	v := ripgrepVersion
	return strings.Join([]string{
		"command -v rg >/dev/null 2>&1 || {",
		"case \"$(uname -m)\" in",
		fmt.Sprintf("aarch64) a=aarch64-unknown-linux-gnu; sha=%s ;;", ripgrepSHA256ARM),
		fmt.Sprintf("*) a=x86_64-unknown-linux-musl; sha=%s ;;", ripgrepSHA256X64),
		"esac;",
		fmt.Sprintf("f=/tmp/ripgrep-%s.tar.gz;", v),
		fmt.Sprintf("curl -fsSL \"https://github.com/BurntSushi/ripgrep/releases/download/%s/ripgrep-%s-$a.tar.gz\" -o \"$f\"", v, v),
		"&& echo \"$sha  $f\" | sha256sum -c -",
		"&& tar xzf \"$f\" -C /tmp",
		fmt.Sprintf("&& mv \"/tmp/ripgrep-%s-$a/rg\" /usr/local/bin/rg && chmod +x /usr/local/bin/rg;", v),
		"}",
	}, " ")
}
