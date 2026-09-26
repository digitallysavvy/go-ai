// Command syncbridges re-copies the embedded harness bridge assets in
// pkg/harness/bridges from a TypeScript ai-sdk checkout, and regenerates
// VERSIONS.json.
//
// The five adapters that ship a bridge entry (claude-code, codex, opencode,
// deepagents, acp) must already be BUILT in the checkout -- this tool copies
// packages/<pkg>/dist/bridge/*, it does not invoke pnpm/tsup itself, so the
// checkout stays a pure "read" dependency of the Go build (no Node toolchain
// is required to build go-ai). Build them first, e.g.:
//
//	pnpm install --frozen-lockfile \
//	  --filter "@ai-sdk/harness-claude-code..." \
//	  --filter "@ai-sdk/harness-codex..." \
//	  --filter "@ai-sdk/harness-opencode..." \
//	  --filter "@ai-sdk/harness-deepagents..." \
//	  --filter "@ai-sdk/harness-acp..."
//	pnpm exec turbo build \
//	  --filter=@ai-sdk/harness-claude-code \
//	  --filter=@ai-sdk/harness-codex \
//	  --filter=@ai-sdk/harness-opencode \
//	  --filter=@ai-sdk/harness-deepagents \
//	  --filter=@ai-sdk/harness-acp
//
// githubcopilot and grokbuild have no bridge entry of their own (they reuse
// ACP's at bootstrap-recipe assembly time in host code); this tool copies
// their src/bridge/{package.json,pnpm-lock.yaml,pnpm-workspace.yaml} directly
// since there is nothing to build.
//
// Usage:
//
//	go run ./pkg/harness/bridges/internal/syncbridges \
//	  -ts-dir /path/to/ai \
//	  -out pkg/harness/bridges \
//	  -tag ai@7.0.113 \
//	  -commit 5c830d570e
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// adapterSpec describes how to sync one embedded adapter directory.
type adapterSpec struct {
	// dirName is both the directory name under -out and the Adapter value in
	// bridges.go.
	dirName string
	// npmPackage is the TS package these assets come from, used to read its
	// own package.json version and to build error messages.
	npmPackage string
	// packageDir is npmPackage's directory name under <ts-dir>/packages.
	packageDir string
	// builtBridgeDir is the dist bridge directory to copy from, relative to
	// the package dir, or "" if this adapter has no bundle step (it copies
	// straight from sourceDir instead).
	builtBridgeDir string
	// sourceDir is the src bridge directory, relative to the package dir.
	// Used for adapters with no bundle step, and recorded in the manifest
	// for adapters that do have one.
	sourceDir string
	// files lists the file names to copy. For entries with a bundle step,
	// "index.mjs" (tsup's output name) is renamed to "bridge.mjs" on copy;
	// every other name is copied as-is.
	files []string
	// bridgeEntry is the file name (after the index.mjs -> bridge.mjs
	// rename) that the TS host launches with `node`, or "" if this adapter
	// borrows another adapter's bridge entry.
	bridgeEntry string
}

var adapterSpecs = []adapterSpec{
	{
		dirName:        "claudecode",
		npmPackage:     "@ai-sdk/harness-claude-code",
		packageDir:     "harness-claude-code",
		builtBridgeDir: "dist/bridge",
		sourceDir:      "src/bridge",
		files:          []string{"index.mjs", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml"},
		bridgeEntry:    "bridge.mjs",
	},
	{
		dirName:        "codex",
		npmPackage:     "@ai-sdk/harness-codex",
		packageDir:     "harness-codex",
		builtBridgeDir: "dist/bridge",
		sourceDir:      "src/bridge",
		files:          []string{"index.mjs", "package.json", "pnpm-lock.yaml"},
		bridgeEntry:    "bridge.mjs",
	},
	{
		dirName:        "opencode",
		npmPackage:     "@ai-sdk/harness-opencode",
		packageDir:     "harness-opencode",
		builtBridgeDir: "dist/bridge",
		sourceDir:      "src/bridge",
		files:          []string{"index.mjs", "host-tool-mcp.mjs", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml"},
		bridgeEntry:    "bridge.mjs",
	},
	{
		dirName:        "deepagents",
		npmPackage:     "@ai-sdk/harness-deepagents",
		packageDir:     "harness-deepagents",
		builtBridgeDir: "dist/bridge",
		sourceDir:      "src/bridge",
		files:          []string{"index.mjs", "package.json", "pnpm-lock.yaml"},
		bridgeEntry:    "bridge.mjs",
	},
	{
		dirName:        "acp",
		npmPackage:     "@ai-sdk/harness-acp",
		packageDir:     "harness-acp",
		builtBridgeDir: "dist/bridge",
		sourceDir:      "src/v1/bridge",
		files:          []string{"index.mjs", "host-tool-mcp.mjs", "package.json", "pnpm-lock.yaml"},
		bridgeEntry:    "bridge.mjs",
	},
	{
		// No index.ts / bundle step: githubcopilot installs its own CLI
		// (@github/copilot) alongside ACP's bridge, which is copied
		// separately as the "acp" adapter.
		dirName:    "githubcopilot",
		npmPackage: "@ai-sdk/harness-github-copilot",
		packageDir: "harness-github-copilot",
		sourceDir:  "src/bridge",
		files:      []string{"package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml"},
	},
	{
		dirName:    "grokbuild",
		npmPackage: "@ai-sdk/harness-grok-build",
		packageDir: "harness-grok-build",
		sourceDir:  "src/bridge",
		files:      []string{"package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml"},
	},
}

type manifestFile = map[string]any

func main() {
	tsDir := flag.String("ts-dir", os.Getenv("AI_SDK_TS_DIR"), "path to the ai-sdk TS monorepo checkout (or set AI_SDK_TS_DIR)")
	outDir := flag.String("out", "pkg/harness/bridges", "output directory (the pkg/harness/bridges package)")
	tag := flag.String("tag", "ai@7.0.113", "TS tag these assets are pinned to")
	commit := flag.String("commit", "5c830d570e", "TS commit these assets are pinned to")
	flag.Parse()

	if *tsDir == "" {
		fmt.Fprintln(os.Stderr, "syncbridges: -ts-dir (or AI_SDK_TS_DIR) is required")
		os.Exit(2)
	}

	manifest := manifestFile{
		"$comment": "Generated by pkg/harness/bridges/internal/syncbridges. Do not hand-edit: re-run the tool against a built " + *tag + " checkout and commit the result.",
		"tsTag":    *tag,
		"tsCommit": *commit,
	}
	adapters := manifestFile{}
	manifest["adapters"] = adapters

	for _, spec := range adapterSpecs {
		info, err := syncAdapter(*tsDir, *outDir, spec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "syncbridges: %s: %v\n", spec.dirName, err)
			os.Exit(1)
		}
		adapters[spec.dirName] = info
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "syncbridges: marshal manifest: %v\n", err)
		os.Exit(1)
	}
	data = append(data, '\n')
	versionsPath := filepath.Join(*outDir, "VERSIONS.json")
	if err := os.WriteFile(versionsPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "syncbridges: write %s: %v\n", versionsPath, err)
		os.Exit(1)
	}
	fmt.Printf("syncbridges: wrote %s and %d adapter director%s under %s\n", versionsPath, len(adapterSpecs), plural(len(adapterSpecs)), *outDir)
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// syncAdapter copies spec's files into <outDir>/<spec.dirName> and returns
// its VERSIONS.json entry.
func syncAdapter(tsDir, outDir string, spec adapterSpec) (manifestFile, error) {
	pkgDir := filepath.Join(tsDir, "packages", spec.packageDir)
	pkgJSONPath := filepath.Join(pkgDir, "package.json")
	sourceVersion, err := readPackageVersion(pkgJSONPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", pkgJSONPath, err)
	}

	fromDir := filepath.Join(pkgDir, spec.sourceDir)
	builtFrom := fmt.Sprintf("packages/%s/%s (direct copy, no bundle step)", spec.packageDir, spec.sourceDir)
	if spec.builtBridgeDir != "" {
		fromDir = filepath.Join(pkgDir, spec.builtBridgeDir)
		if _, err := os.Stat(fromDir); err != nil {
			return nil, fmt.Errorf(
				"%s does not exist -- build %s first (see this tool's package doc for the exact `pnpm`/`turbo` commands): %w",
				fromDir, spec.npmPackage, err)
		}
		builtFrom = fmt.Sprintf("packages/%s/%s (tsup bundle of packages/%s/%s)", spec.packageDir, spec.builtBridgeDir, spec.packageDir, spec.sourceDir)
	}

	destDir := filepath.Join(outDir, spec.dirName)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}

	files := manifestFile{}
	for _, name := range spec.files {
		srcPath := filepath.Join(fromDir, name)
		destName := name
		if name == "index.mjs" {
			destName = "bridge.mjs"
		}
		destPath := filepath.Join(destDir, destName)
		sum, err := copyFile(srcPath, destPath)
		if err != nil {
			return nil, fmt.Errorf("copying %s: %w", srcPath, err)
		}
		files[destName] = sum
	}

	return manifestFile{
		"sourcePackage": spec.npmPackage,
		"sourceVersion": sourceVersion,
		"sourceDir":     fmt.Sprintf("packages/%s/%s", spec.packageDir, spec.sourceDir),
		"builtFrom":     builtFrom,
		"bridgeEntry":   spec.bridgeEntry,
		"files":         sortedKeys(files),
	}, nil
}

// sortedKeys re-marshals a manifestFile of file->hash pairs so
// json.MarshalIndent emits keys in a stable, sorted order (Go's map
// iteration in encoding/json already sorts string keys, so this is really
// just documentation of that behavior via a typed pass-through).
func sortedKeys(files manifestFile) manifestFile {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return files
}

func copyFile(srcPath, destPath string) (sha256Hex string, err error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer src.Close()

	dest, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer dest.Close()

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(dest, hasher), src); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func readPackageVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", err
	}
	return pkg.Version, nil
}
