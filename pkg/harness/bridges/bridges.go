// Package bridges embeds the in-sandbox Node bridge assets that each TS
// harness adapter ships (its bundled `bridge.mjs`, `package.json` and pnpm
// lockfile/workspace files), pinned to a specific ai@<tag> release of the
// TypeScript AI SDK.
//
// These assets are the "bridge-inherited" half of the harness parity surface:
// the Go host never re-implements the in-sandbox agent-runtime glue (Claude
// Code SDK usage, Codex/OpenCode/DeepAgents CLIs, ACP relaying, JSON-schema
// translation, stream-event emission, ...). Instead the Go bootstrap code
// (pkg/harness's adapter packages, see bootstrap.go / Bootstrap) copies these
// exact bytes into the sandbox next to a `package.json` + lockfile, runs
// `pnpm install --frozen-lockfile`, and then launches `node bridge.mjs`
// exactly like the TS host does. Any TS-only change that lives solely inside
// the bridge (see state/parity/sep_23_2026/harness.md, WG6 rows marked
// "Bridge-inherited") is therefore picked up for free by re-syncing these
// files -- no Go host code is required.
//
// Regeneration: run `go run ./pkg/harness/bridges/internal/syncbridges
// -ts-dir <path-to-ai-checkout>` against a *built* ai/ checkout (i.e. one
// where `pnpm turbo build --filter=@ai-sdk/harness-claude-code
// --filter=@ai-sdk/harness-codex --filter=@ai-sdk/harness-opencode
// --filter=@ai-sdk/harness-deepagents --filter=@ai-sdk/harness-acp` has
// already produced `dist/bridge/*` for each adapter), then commit the
// updated files under this directory together with VERSIONS.json.
package bridges

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
)

// go generate reads AI_SDK_TS_DIR from the environment; go:generate lines run
// without a shell, so the substitution needs an explicit `sh -c`.
//go:generate sh -c "go run ./internal/syncbridges -ts-dir \"$AI_SDK_TS_DIR\" -out ."

//go:embed VERSIONS.json
//go:embed claudecode/* codex/* opencode/* deepagents/* acp/* githubcopilot/* grokbuild/*
var assets embed.FS

// Adapter identifies one embedded bridge asset set. The value is also the
// directory name under this package and the TS harness id used in bootstrap
// recipes (e.g. "claude-code" in TS becomes the "claudecode" directory here;
// see Manifest().Adapters[x].SourcePackage for the exact TS package name).
type Adapter string

// The adapters embedded by this package. Cline and Pi are intentionally
// excluded (TS-only by user decision; see harness.md WG6). Cursor and fx have
// no bridge assets of their own -- like githubcopilot and grokbuild, they are
// thin ACP-recipe configs that reuse the ACP bridge entry at bootstrap-recipe
// assembly time in host code (out of scope for this package).
const (
	ClaudeCode    Adapter = "claudecode"
	Codex         Adapter = "codex"
	OpenCode      Adapter = "opencode"
	DeepAgents    Adapter = "deepagents"
	ACP           Adapter = "acp"
	GitHubCopilot Adapter = "githubcopilot"
	GrokBuild     Adapter = "grokbuild"
)

// Adapters lists every embedded adapter, in a stable order.
func Adapters() []Adapter {
	return []Adapter{ClaudeCode, Codex, OpenCode, DeepAgents, ACP, GitHubCopilot, GrokBuild}
}

// FS returns the embedded filesystem. Each adapter's files live directly
// under its directory (e.g. "claudecode/bridge.mjs"), plus the shared
// "VERSIONS.json" manifest at the root.
func FS() embed.FS { return assets }

// Files returns every embedded file for adapter as a map of file name (not
// path -- just "bridge.mjs", "package.json", ...) to its raw bytes. It
// returns an error if adapter has no embedded directory.
func Files(adapter Adapter) (map[string][]byte, error) {
	dir := string(adapter)
	entries, err := fs.ReadDir(assets, dir)
	if err != nil {
		return nil, fmt.Errorf("bridges: unknown adapter %q: %w", adapter, err)
	}
	out := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := fs.ReadFile(assets, dir+"/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("bridges: reading %s/%s: %w", dir, entry.Name(), err)
		}
		out[entry.Name()] = data
	}
	return out, nil
}

// FileNames returns the sorted list of embedded file names for adapter.
func FileNames(adapter Adapter) ([]string, error) {
	files, err := Files(adapter)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// BridgeEntry returns the embedded bridge entry point's contents for adapter
// (its bundled "bridge.mjs"), and whether adapter has one at all.
// GitHubCopilot and GrokBuild have no bridge entry of their own: they borrow
// ACP's at bootstrap-recipe assembly time in host code, and BridgeEntry
// returns ok=false for them.
func BridgeEntry(adapter Adapter) (data []byte, ok bool, err error) {
	m, err := Manifest()
	if err != nil {
		return nil, false, err
	}
	info, known := m.Adapters[string(adapter)]
	if !known || info.BridgeEntry == "" {
		return nil, false, nil
	}
	data, err = fs.ReadFile(assets, string(adapter)+"/"+info.BridgeEntry)
	if err != nil {
		return nil, false, fmt.Errorf("bridges: reading bridge entry for %q: %w", adapter, err)
	}
	return data, true, nil
}
