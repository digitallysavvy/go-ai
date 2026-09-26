package bridges

import (
	"encoding/json"
	"fmt"
)

// AdapterManifest is one adapter's entry in VERSIONS.json.
type AdapterManifest struct {
	// SourcePackage is the TS npm package the assets were built from (e.g.
	// "@ai-sdk/harness-claude-code").
	SourcePackage string `json:"sourcePackage"`
	// SourceVersion is that package's own semver at the pinned TS tag.
	SourceVersion string `json:"sourceVersion"`
	// SourceDir is the TS repo-relative directory the bridge sources live in.
	SourceDir string `json:"sourceDir"`
	// BuiltFrom documents how the embedded bytes were produced (a tsup
	// bundle of the source, or a direct copy when there is no bundle step).
	BuiltFrom string `json:"builtFrom"`
	// BridgeEntry is the file name (relative to the adapter directory) that
	// the TS host launches with `node`, or "" if this adapter has none of
	// its own (it borrows another adapter's bridge entry; see githubcopilot
	// / grokbuild, which reuse ACP's).
	BridgeEntry string `json:"bridgeEntry"`
	// Files maps each embedded file name to its lowercase-hex sha256.
	Files map[string]string `json:"files"`
}

// ManifestData is the parsed contents of VERSIONS.json.
type ManifestData struct {
	TSTag    string                     `json:"tsTag"`
	TSCommit string                     `json:"tsCommit"`
	Adapters map[string]AdapterManifest `json:"adapters"`
}

// Manifest parses and returns the embedded VERSIONS.json.
func Manifest() (ManifestData, error) {
	var m ManifestData
	data, err := assets.ReadFile("VERSIONS.json")
	if err != nil {
		return m, fmt.Errorf("bridges: reading VERSIONS.json: %w", err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("bridges: parsing VERSIONS.json: %w", err)
	}
	return m, nil
}
