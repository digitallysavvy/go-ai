package xai

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Ports of TS 565db6d2c8 (declare xAI language model capabilities per model
// ID): the matrix is hand-ported and compared row by row with the TS source
// file when the TS checkout is present.

func portprovbXAITSCatalogSource(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("packages", "xai", "src", "xai-model-catalog.ts")
	var candidates []string
	if repo := os.Getenv("GOAI_TS_REPO"); repo != "" {
		candidates = append(candidates, filepath.Join(repo, rel))
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for ; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		candidates = append(candidates, filepath.Join(dir, "ai", rel))
	}
	for _, path := range candidates {
		if raw, err := os.ReadFile(path); err == nil {
			return string(raw)
		}
	}
	t.Skip("Skipping: TS checkout (ai/packages/xai/src/xai-model-catalog.ts) not found; set GOAI_TS_REPO")
	return ""
}

var portprovbXAIRowRe = regexp.MustCompile(`(?s)\n  '([^']+)': \{\n    documented: (true|false),\n    image: ([^,\n]+),\n    reasoningEfforts: ([^\n]+?),\n  \},`)

func portprovbParseXAIMatrix(t *testing.T, source string) ([]string, map[string]xaiLanguageModelRow) {
	t.Helper()
	const header = "const languageModelDefinitions = {"
	start := strings.Index(source, header)
	end := strings.Index(source, "} satisfies Record<string, XaiLanguageModelRow>;")
	if start < 0 || end < start {
		t.Fatal("languageModelDefinitions not found in the TS catalog")
	}
	body := source[start+len(header) : end]
	var ids []string
	rows := map[string]xaiLanguageModelRow{}
	for _, m := range portprovbXAIRowRe.FindAllStringSubmatch(body, -1) {
		row := xaiLanguageModelRow{documented: m[2] == "true", image: strings.Trim(strings.TrimSpace(m[3]), "'")}
		if strings.TrimSpace(m[4]) != "false" {
			row.reasons = true
			row.reasoningEfforts = []string{}
			for _, e := range regexp.MustCompile(`'(\w+)'`).FindAllStringSubmatch(m[4], -1) {
				row.reasoningEfforts = append(row.reasoningEfforts, e[1])
			}
		}
		ids = append(ids, m[1])
		rows[m[1]] = row
	}
	if got := strings.Count(body, "documented:"); got != len(ids) {
		t.Fatalf("parsed %d rows but the source has %d", len(ids), got)
	}
	return ids, rows
}

// TS 565db6d2c8: every TS matrix row is in the Go matrix with the same
// columns, and the Go matrix has no extra row.
func TestPortprovbXAICapabilityMatrixMatchesTS(t *testing.T) {
	ids, want := portprovbParseXAIMatrix(t, portprovbXAITSCatalogSource(t))
	if len(ids) < 16 {
		t.Fatalf("parsed %d TS rows, want at least 16", len(ids))
	}
	for _, id := range ids {
		got, ok := xaiLanguageModelMatrix[id]
		if !ok {
			t.Errorf("%s: row missing from the Go matrix", id)
			continue
		}
		if !reflect.DeepEqual(got, want[id]) {
			t.Errorf("%s:\n got  %+v\n want %+v", id, got, want[id])
		}
	}
	var goIDs []string
	for id := range xaiLanguageModelMatrix {
		goIDs = append(goIDs, id)
	}
	sort.Strings(goIDs)
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(goIDs, sorted) {
		t.Errorf("Go matrix IDs differ from the TS matrix:\n go %v\n ts %v", goIDs, sorted)
	}
}

// The portable efforts of an unlisted model come from the family rules, and a
// listed model's row wins over them (a dated snapshot of a listed model is an
// unlisted ID).
func TestPortprovbXAIPortableEfforts(t *testing.T) {
	cases := map[string][]string{
		"grok-4.3":                 {"none", "low", "medium", "high", "xhigh"},
		"grok-4.7":                 {"low", "medium", "high", "xhigh"},
		"grok-4.20-reasoning":      nil,
		"grok-4.20-non-reasoning":  nil,
		"grok-4.20-0309-reasoning": nil, // unlisted, family rule
		"grok-4.20-multi-agent":    {"none", "low", "medium", "high"},
		"grok-5":                   {"low", "medium", "high"},
		"grok-9-custom":            {"low", "medium", "high"},
		"custom-model":             {"none", "low", "medium", "high"},
		"grok-4-1":                 {"none", "low", "medium", "high"},
	}
	for id, want := range cases {
		if got := xaiPortableEfforts(id); !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
			t.Errorf("%s: efforts = %v, want %v", id, got, want)
		}
	}
}
