package gemini

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// portprovgCatalogPath finds google-model-catalog.ts in the TS checkout:
// $AI_SDK_TS_DIR (repo root or packages/google/src), else an `ai` directory
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
		for _, rel := range []string{filepath.Join("packages", "google", "src"), ""} {
			path := filepath.Join(root, rel, "google-model-catalog.ts")
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}
	return ""
}

type portprovgRow struct {
	id, types, body string
}

// portprovgParseRows reads the `modelDefinitions` rows: id, types and the
// language row text ("" for a model without one).
func portprovgParseRows(t *testing.T, src string) []portprovgRow {
	t.Helper()
	start := strings.Index(src, "const modelDefinitions = {")
	end := strings.Index(src, "\n} as const satisfies Record<string, GoogleModelDefinition>")
	if start < 0 || end < start {
		t.Fatal("modelDefinitions not found in the TS catalog")
	}
	block := src[start:end]
	entry := regexp.MustCompile(`(?s)\n  '([^']+)': \{\n    types: \[([^\]]*)\],(.*?)\n  \},`)
	var rows []portprovgRow
	for _, m := range entry.FindAllStringSubmatch(block, -1) {
		rows = append(rows, portprovgRow{id: m[1], types: m[2], body: m[3]})
	}
	return rows
}

func portprovgField(body, name string) string {
	m := regexp.MustCompile(`(?s)\n      ` + name + `: (.*?),\n      [a-zA-Z]+:|\n      ` + name + `: (.*?),\n    \}`).FindStringSubmatch(body + "\n    }")
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

func portprovgStrings(s string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// TS 1145cd58eb / 4e43721f73 / 875de7bb27: every listed language model row of
// the TS catalog resolves in Go to the same request-relevant cells. Skipped
// without the TS checkout.
func TestPortprovgLanguageCatalogMatchesTS(t *testing.T) {
	path := portprovgCatalogPath()
	if path == "" {
		t.Skip("TS checkout (google-model-catalog.ts) not found; set AI_SDK_TS_DIR")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows := portprovgParseRows(t, string(raw))
	checked := 0
	for _, row := range rows {
		if !strings.Contains(row.body, "language: {") {
			continue
		}
		checked++
		got := LanguageModelCapabilitiesFor(row.id)
		fnTools := portprovgField(row.body, "functionTools")
		features := portprovgStrings(fnTools)
		has := func(f string) bool {
			for _, v := range features {
				if v == f {
					return true
				}
			}
			return false
		}
		providerTools := strings.Trim(portprovgField(row.body, "providerTools"), "'")
		levels := portprovgField(row.body, "thinkingLevels")
		minB, _ := strconv.Atoi(portprovgField(row.body, "minThinkingBudget"))
		maxB, _ := strconv.Atoi(portprovgField(row.body, "maxThinkingBudget"))
		penalties := portprovgField(row.body, "penalties")
		systemInstruction := strings.Trim(portprovgField(row.body, "systemInstruction"), "'")

		want := LanguageModelCapabilities{
			ProviderTools:           providerTools != "false",
			FileSearch:              providerTools == "with-file-search",
			FunctionTools:           fnTools != "false",
			Mixed:                   has("with-provider-tools"),
			FunctionResponseParts:   has("multimodal-responses"),
			ThoughtSignatures:       has("thought-signatures"),
			WithResponseSchema:      has("with-response-schema"),
			JSONResponseTool:        has("json-response-tool"),
			MinThinkingBudget:       minB,
			MaxThinkingBudget:       maxB,
			PenaltiesVertexOnly:     penalties != "true",
			SystemInstructionAsUser: systemInstruction == "user",
			ExternalFileURLs:        portprovgField(row.body, "externalFileUrls") == "true",
		}
		if levels != "undefined" {
			want.ThinkingLevels = portprovgStrings(levels)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got  %+v\n want %+v", row.id, got, want)
		}
	}
	if checked < 20 {
		t.Fatalf("only %d language rows parsed from the TS catalog", checked)
	}
	t.Logf("%d language rows compared", checked)
}

// Ruling 13 (differential test on unknown model IDs): the fallback for IDs
// that are not listed in the TS catalog (prefixed IDs, Vertex paths, future
// Gemini versions) resolves exactly like the per-ID rules it replaced (TS
// 1145cd58eb resolveUnlistedLanguageModelRow).
func TestPortprovgUnlistedLanguageModelFallback(t *testing.T) {
	levels := func(l ...string) []string { return l }
	cases := []struct {
		id   string
		want LanguageModelCapabilities
	}{
		{"gemini-99-pro-preview", LanguageModelCapabilities{
			ProviderTools: true, FileSearch: true, FunctionTools: true, Mixed: true, FunctionResponseParts: true, ThoughtSignatures: true,
			WithResponseSchema: true, JSONResponseTool: true, ThinkingLevels: levels("low", "medium", "high"), MaxThinkingBudget: 24576, ExternalFileURLs: true,
		}},
		{"eu.gemini-2.5-flash", LanguageModelCapabilities{
			ProviderTools: true, FileSearch: true, FunctionTools: true, PenaltiesVertexOnly: true, MaxThinkingBudget: 24576,
		}},
		{"gemini-2.5-pro", LanguageModelCapabilities{
			ProviderTools: true, FileSearch: true, FunctionTools: true, JSONResponseTool: true, PenaltiesVertexOnly: true,
			MinThinkingBudget: 128, MaxThinkingBudget: 32768, ExternalFileURLs: true,
		}},
		{"gemma-3-27b-it", LanguageModelCapabilities{FunctionTools: true, SystemInstructionAsUser: true, MaxThinkingBudget: 24576}},
		{"models/gemini-4-hydrogen-eap", LanguageModelCapabilities{
			ProviderTools: true, FileSearch: true, FunctionTools: true, Mixed: true, FunctionResponseParts: true, ThoughtSignatures: true,
			WithResponseSchema: true, JSONResponseTool: true, ThinkingLevels: levels("low", "medium", "high", "xhigh"), MaxThinkingBudget: 24576, ExternalFileURLs: true,
		}},
		{"gemini-4-hydrogenous", LanguageModelCapabilities{
			ProviderTools: true, FileSearch: true, FunctionTools: true, Mixed: true, FunctionResponseParts: true, ThoughtSignatures: true,
			WithResponseSchema: true, JSONResponseTool: true, ThinkingLevels: levels("minimal", "low", "medium", "high"), MaxThinkingBudget: 24576, ExternalFileURLs: true,
		}},
		{"gemini-3-pro-image-preview-x", LanguageModelCapabilities{
			ProviderTools: true, FileSearch: true, FunctionTools: true, Mixed: true, FunctionResponseParts: true, ThoughtSignatures: true,
			WithResponseSchema: true, JSONResponseTool: true, MaxThinkingBudget: 32768, ExternalFileURLs: true,
		}},
		{"nano-banana-pro-preview", LanguageModelCapabilities{ProviderTools: true, FunctionTools: true, MaxThinkingBudget: 24576}},
		{"gemini-1.5-flash", LanguageModelCapabilities{FunctionTools: true, JSONResponseTool: true, MaxThinkingBudget: 24576, ExternalFileURLs: true}},
	}
	for _, tc := range cases {
		if got := LanguageModelCapabilitiesFor(tc.id); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got  %+v\n want %+v", tc.id, got, tc.want)
		}
	}
}
