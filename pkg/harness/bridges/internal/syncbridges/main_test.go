package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsMatchVersionsJSON(t *testing.T) {
	data, err := os.ReadFile("../../VERSIONS.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		TsTag    string `json:"tsTag"`
		TsCommit string `json:"tsCommit"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.TsTag != defaultTag || m.TsCommit != defaultCommit {
		t.Fatalf("defaults %s/%s != VERSIONS.json %s/%s", defaultTag, defaultCommit, m.TsTag, m.TsCommit)
	}
}

// TS 726a1ccb32: a built dist/bridge ships pnpm-lockfile.yaml; the embedded
// copy keeps the pnpm-lock.yaml name. Direct-copy adapters still read
// src/bridge/pnpm-lock.yaml.
func TestSyncAdapterReadsDistLockfileName(t *testing.T) {
	ts := t.TempDir()
	out := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(ts, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("packages/harness-codex/package.json", `{"version":"9.9.9"}`)
	write("packages/harness-codex/dist/bridge/index.mjs", "bundle")
	write("packages/harness-codex/dist/bridge/package.json", "{}")
	write("packages/harness-codex/dist/bridge/pnpm-lockfile.yaml", "lock")
	write("packages/harness-grok-build/package.json", `{"version":"1.2.3"}`)
	for _, f := range []string{"package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml"} {
		write("packages/harness-grok-build/src/bridge/"+f, f)
	}

	for _, spec := range adapterSpecs {
		if spec.dirName != "codex" && spec.dirName != "grokbuild" {
			continue
		}
		info, err := syncAdapter(ts, out, spec)
		if err != nil {
			t.Fatalf("%s: %v", spec.dirName, err)
		}
		files := info["files"].(manifestFile)
		if _, ok := files["pnpm-lock.yaml"]; !ok {
			t.Fatalf("%s: manifest files = %v, want pnpm-lock.yaml", spec.dirName, files)
		}
	}
	got, err := os.ReadFile(filepath.Join(out, "codex", "pnpm-lock.yaml"))
	if err != nil || string(got) != "lock" {
		t.Fatalf("codex pnpm-lock.yaml = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(out, "codex", "pnpm-lockfile.yaml")); !os.IsNotExist(err) {
		t.Fatalf("pnpm-lockfile.yaml must not be embedded: %v", err)
	}
	got, err = os.ReadFile(filepath.Join(out, "grokbuild", "pnpm-lock.yaml"))
	if err != nil || string(got) != "pnpm-lock.yaml" {
		t.Fatalf("grokbuild pnpm-lock.yaml = %q, %v", got, err)
	}
}
