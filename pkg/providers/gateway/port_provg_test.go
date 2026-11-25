package gateway

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// portprovgCatalogPath finds gateway-model-catalog.ts in the TS checkout:
// $AI_SDK_TS_DIR (repo root or packages/gateway/src), else an `ai` directory
// next to one of this directory's ancestors. "" when there is none.
func portprovgCatalogPath() string {
	var roots []string
	if dir := os.Getenv("AI_SDK_TS_DIR"); dir != "" {
		roots = append(roots, dir)
	}
	if wd, err := os.Getwd(); err == nil {
		for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			roots = append(roots, filepath.Join(dir, "ai"))
		}
	}
	for _, root := range roots {
		for _, rel := range []string{filepath.Join("packages", "gateway", "src"), ""} {
			path := filepath.Join(root, rel, "gateway-model-catalog.ts")
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}
	return ""
}

// TS 640569da18, 9baa2d0fd7, 74042c5c65, d03d74ad2f, 700089c048, beccad3766,
// f2732a66f7 (gateway model settings syncs) and the catalog refactors
// a51981cef1 / f5fc6ba3f1: every kind's list equals the TS catalog.
func TestPortprovgGatewayCatalogMatchesTS(t *testing.T) {
	path := portprovgCatalogPath()
	if path == "" {
		t.Skip("TS checkout (gateway-model-catalog.ts) not found; set AI_SDK_TS_DIR")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	literal := regexp.MustCompile(`'([^'\\]*)'`)
	all := catalogs()
	all["extraction"] = nil
	for _, id := range GatewayExtractionModelIDs {
		all["extraction"] = append(all["extraction"], string(id))
	}
	for kind, got := range all {
		m := regexp.MustCompile(`(?s)\n  ` + kind + `:\s*\[(.*?)\]`).FindStringSubmatch(string(raw))
		if m == nil {
			t.Errorf("%s: not in the TS catalog", kind)
			continue
		}
		var want []string
		for _, lit := range literal.FindAllStringSubmatch(m[1], -1) {
			want = append(want, lit[1])
		}
		g := append([]string(nil), got...)
		sort.Strings(g)
		sort.Strings(want)
		if strings.Join(g, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s list differs from TS catalog: Go %d IDs, TS %d IDs", kind, len(g), len(want))
		}
	}
}

// New IDs are suggested; removed IDs leave the lists but stay as deprecated
// constants so v0.5.2 callers keep compiling.
func TestPortprovgGatewayModelIDChanges(t *testing.T) {
	all := catalogs()
	has := func(kind, id string) bool {
		for _, v := range all[kind] {
			if v == id {
				return true
			}
		}
		return false
	}
	for kind, ids := range map[string][]string{
		"language": {"google/gemini-4-hydrogen", "google/gemini-4-neon", "openai/gpt-6.2-astra", "openai/gpt-6.2-astra-fast", "openai/gpt-6.2-sol", "openai/gpt-6.2-sol-fast", "alibaba/qwen4-max"},
		"decision": {"google/gemini-decision-1", "heygen/heygen-decision-1", "heygen/heygen-decision-1-turbo", "liquid/d1.2", "perplexity/pplx-decider-v1.3"},
		"image":    {"bytedance/seedream-5.5", "minimax/minimax-h3-image"},
		"video":    {"bytedance/seedance-2.5-flash"},
		"realtime": {"openai/gpt-live-1.1"},
	} {
		for _, id := range ids {
			if !has(kind, id) {
				t.Errorf("%s catalog missing %q", kind, id)
			}
		}
	}
	if got := GatewayExtractionModelIDs; len(got) != 1 || got[0] != "scaledown/extract-1" {
		t.Errorf("extraction IDs = %v", got)
	}
	for _, removed := range []struct{ kind, id string }{
		{"language", string(GatewayLanguageModelSpacexaiGrokBuild01)},
		{"language", string(GatewayLanguageModelAlibabaQwen3Coder)},
		{"language", string(GatewayLanguageModelInceptionMercury2)},
		{"decision", string(GatewayDecisionModelLiquidD1)},
		{"image", string(GatewayImageModelProdiaFluxFastSchnell)},
		{"video", string(GatewayVideoModelBytedanceSeedanceV15Pro)},
	} {
		if has(removed.kind, removed.id) {
			t.Errorf("%s catalog still lists removed %q", removed.kind, removed.id)
		}
	}
}
