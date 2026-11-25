package openai

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports of TS c3ee424a70 (declare language model capabilities per model ID):
// the matrix is hand-ported and checked row by row against the TS source file
// when the TS checkout is present.

// portprovbTSCatalogSource returns the text of the TS openai-model-catalog.ts,
// from $GOAI_TS_REPO or an `ai` checkout next to one of the parent
// directories; the test skips when there is none.
func portprovbTSCatalogSource(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("packages", "openai", "src", "openai-model-catalog.ts")
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
	t.Skip("Skipping: TS checkout (ai/packages/openai/src/openai-model-catalog.ts) not found; set GOAI_TS_REPO")
	return ""
}

var (
	portprovbRowStartRe = regexp.MustCompile(`(?m)^  ('[^']+'|[\w\-.]+): \{$`)
	portprovbQuotedRe   = regexp.MustCompile(`'([^']*)'`)
)

// portprovbParseTSMatrix parses openaiModelDefinitions into rows keyed by
// model ID, in TS order. Only the columns the Go row carries are read.
func portprovbParseTSMatrix(t *testing.T, source string) ([]string, map[string]openAILanguageModelRow) {
	t.Helper()
	const header = "export const openaiModelDefinitions = {"
	start := strings.Index(source, header)
	end := strings.Index(source, "} as const satisfies Record<string, OpenAILanguageModelRow>;")
	if start < 0 || end < start {
		t.Fatal("openaiModelDefinitions not found in the TS catalog")
	}
	body := source[start+len(header) : end]
	starts := portprovbRowStartRe.FindAllStringSubmatchIndex(body, -1)
	var ids []string
	rows := map[string]openAILanguageModelRow{}
	for i, loc := range starts {
		id := strings.Trim(body[loc[2]:loc[3]], "'")
		rowEnd := len(body)
		if i+1 < len(starts) {
			rowEnd = starts[i+1][0]
		}
		text := body[loc[1]:rowEnd]
		field := func(name string) string {
			m := regexp.MustCompile(`(?m)^    ` + name + `: (.*?),?$`).FindStringSubmatch(text)
			if m == nil {
				t.Fatalf("%s: column %s missing", id, name)
			}
			return m[1]
		}
		boolField := func(name string) bool {
			switch v := field(name); v {
			case "true":
				return true
			case "false":
				return false
			default:
				t.Fatalf("%s: column %s = %s, want a boolean", id, name, v)
				return false
			}
		}
		strs := func(name string) []string {
			var out []string
			for _, m := range portprovbQuotedRe.FindAllStringSubmatch(field(name), -1) {
				out = append(out, m[1])
			}
			return out
		}
		row := openAILanguageModelRow{
			endpoints:                            strs("endpoints"),
			documented:                           boolField("documented"),
			samplingAtReasoningEffortNone:        boolField("samplingAtReasoningEffortNone"),
			asyncTools:                           boolField("asyncTools"),
			flexProcessing:                       boolField("flexProcessing"),
			priorityProcessing:                   boolField("priorityProcessing"),
			configurationUpdates:                 boolField("configurationUpdates"),
			promptCacheRetention:                 boolField("promptCacheRetention"),
			promptCacheOptions:                   boolField("promptCacheOptions"),
			chatTemperature:                      boolField("chatTemperature"),
			chatFunctionToolsWithReasoning:       boolField("chatFunctionToolsWithReasoning"),
			responsesLogprobsRequireNonReasoning: boolField("responsesLogprobsRequireNonReasoning"),
			systemMessageMode:                    strings.Trim(field("systemMessageMode"), "'"),
		}
		if field("reasoningEfforts") != "undefined" {
			row.reasoningEfforts = strs("reasoningEfforts")
		}
		ids = append(ids, id)
		rows[id] = row
	}
	return ids, rows
}

// TS c3ee424a70: every TS matrix row is in the Go matrix with the same
// columns, and the Go matrix has no extra row.
func TestPortprovbCapabilityMatrixMatchesTS(t *testing.T) {
	ids, want := portprovbParseTSMatrix(t, portprovbTSCatalogSource(t))
	if len(ids) < 80 {
		t.Fatalf("parsed %d TS rows, want at least 80", len(ids))
	}
	for _, id := range ids {
		got, ok := openAILanguageModelMatrix[id]
		if !ok {
			t.Errorf("%s: row missing from the Go matrix", id)
			continue
		}
		if !reflect.DeepEqual(got, want[id]) {
			t.Errorf("%s:\n got  %+v\n want %+v", id, got, want[id])
		}
	}
	var goIDs []string
	for id := range openAILanguageModelMatrix {
		goIDs = append(goIDs, id)
	}
	sort.Strings(goIDs)
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(goIDs, sorted) {
		t.Errorf("Go matrix IDs differ from the TS matrix:\n go %v\n ts %v", goIDs, sorted)
	}
}

// Self-check of the differential parser: it must see the columns it compares.
func TestPortprovbCapabilityMatrixParserSeesColumns(t *testing.T) {
	source := `export const openaiModelDefinitions = {
  'a-model': {
    endpoints: ['chat'],
    documented: true,
    reasoningEfforts: ['low', 'high'],
    samplingAtReasoningEffortNone: true,
    temperature: atReasoningEffortNone,
    asyncTools: false,
    flexProcessing: true,
    priorityProcessing: false,
    configurationUpdates: false,
    promptCacheRetention: true,
    promptCacheOptions: false,
    chatTemperature: true,
    chatFunctionToolsWithReasoning: false,
    responsesLogprobsRequireNonReasoning: true,
    systemMessageMode: 'developer',
  },
  o1: {
    endpoints: [],
    documented: false,
    reasoningEfforts: undefined,
    samplingAtReasoningEffortNone: false,
    asyncTools: false,
    flexProcessing: false,
    priorityProcessing: false,
    configurationUpdates: false,
    promptCacheRetention: true,
    promptCacheOptions: false,
    chatTemperature: true,
    chatFunctionToolsWithReasoning: true,
    responsesLogprobsRequireNonReasoning: false,
    systemMessageMode: 'system',
  },
} as const satisfies Record<string, OpenAILanguageModelRow>;`
	ids, rows := portprovbParseTSMatrix(t, source)
	if !reflect.DeepEqual(ids, []string{"a-model", "o1"}) {
		t.Fatalf("ids = %v", ids)
	}
	want := openAILanguageModelRow{
		endpoints: []string{"chat"}, documented: true, reasoningEfforts: []string{"low", "high"}, samplingAtReasoningEffortNone: true,
		flexProcessing: true, promptCacheRetention: true, chatTemperature: true, responsesLogprobsRequireNonReasoning: true, systemMessageMode: "developer",
	}
	if !reflect.DeepEqual(rows["a-model"], want) {
		t.Fatalf("row = %+v", rows["a-model"])
	}
	if rows["o1"].reasoningEfforts != nil || rows["o1"].systemMessageMode != "system" {
		t.Fatalf("o1 = %+v", rows["o1"])
	}
}

// TS 'supports the documented %s capabilities' (gpt-6.2-astra, gpt-6.2-sol).
func TestPortprovbGPT62Capabilities(t *testing.T) {
	for _, id := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		got := GetLanguageModelCapabilities(id)
		want := LanguageModelCapabilities{
			IsReasoningModel:                        true,
			SystemMessageMode:                       "developer",
			SupportsFlexProcessing:                  true,
			SupportsPriorityProcessing:              true,
			SupportsConfigurationUpdate:             true,
			SupportsAsyncToolCalling:                true,
			SupportedReasoningEfforts:               []string{"low", "medium", "high", "xhigh", "max"},
			SupportsNonReasoningParameters:          false,
			SupportsChatFunctionToolsWithReasoning:  true,
			SupportsPromptCacheRetention:            false,
			SupportsChatTemperatureWithoutReasoning: true,
			ResponsesLogprobsRequireNonReasoning:    true,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %+v, want %+v", id, got, want)
		}
	}
}

// The ID resolution: a dated snapshot of a listed model uses that model's row;
// an unlisted ID uses the generic reasoning row (o-series, GPT-5 and later
// except GPT-5 chat models) or the generic non-reasoning row.
func TestPortprovbLanguageModelRowResolution(t *testing.T) {
	cases := []struct {
		id        string
		reasoning bool
		efforts   []string
	}{
		{"gpt-5.4-2026-03-05", true, []string{"none", "low", "medium", "high", "xhigh"}},
		{"gpt-5.99", true, []string{"low", "medium", "high", "xhigh", "max"}},
		{"gpt-5.99-chat-latest", true, []string{"low", "medium", "high", "xhigh", "max"}},
		{"gpt-5-chat-2099-01-01", false, nil},
		{"gpt-5-foo-chat", true, []string{"low", "medium", "high", "xhigh", "max"}},
		{"o99-2099-01-01", true, []string{"low", "medium", "high", "xhigh", "max"}},
		{"gpt-4o-search-preview-2099-01-01", false, nil},
		{"ft:gpt-4o-2024-08-06:org:custom:abc123", false, nil},
		{"new-unknown-model", false, nil},
	}
	for _, tc := range cases {
		got := GetLanguageModelCapabilities(tc.id)
		if got.IsReasoningModel != tc.reasoning || !stringSlicesEqual(got.SupportedReasoningEfforts, tc.efforts) {
			t.Errorf("%s: reasoning=%v efforts=%v, want %v %v", tc.id, got.IsReasoningModel, got.SupportedReasoningEfforts, tc.reasoning, tc.efforts)
		}
	}
	// The search preview models reject temperature; an unlisted ID that only
	// starts with the same text no longer does (TS temperatureWithoutReasoning
	// is a row column, not a prefix test).
	for id, want := range map[string]bool{
		"gpt-4o-search-preview":            false,
		"gpt-4o-mini-search-preview":       false,
		"gpt-4o-search-preview-2025-03-11": false,
		"gpt-4o-search-preview-custom":     true,
		"gpt-4o":                           true,
	} {
		if got := GetLanguageModelCapabilities(id).SupportsChatTemperatureWithoutReasoning; got != want {
			t.Errorf("%s: chat temperature = %v, want %v", id, got, want)
		}
	}
}

// TS responses 'should map top-level reasoning=%s to reasoning effort %s'
// (c3ee424a70): o3-mini accepts low, medium and high; other values map to the
// nearest supported effort with a compatibility warning.
func TestPortprovbResponsesTopLevelReasoningMapsToSupportedEffort(t *testing.T) {
	cases := []struct {
		level  types.ReasoningLevel
		effort string
	}{
		{types.ReasoningNone, "low"},
		{types.ReasoningMinimal, "low"},
		{types.ReasoningLow, "low"},
		{types.ReasoningMedium, "medium"},
		{types.ReasoningHigh, "high"},
		{types.ReasoningXHigh, "high"},
	}
	for _, tc := range cases {
		level := tc.level
		m := NewResponsesLanguageModel(New(Config{APIKey: "test-key"}), "o3-mini")
		body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
			Prompt:    types.Prompt{Messages: []types.Message{userMsg("Hello")}},
			Reasoning: &level,
		}, false)
		if err != nil {
			t.Fatal(err)
		}
		reasoning, _ := body["reasoning"].(map[string]interface{})
		if reasoning["effort"] != tc.effort || reasoning["summary"] != "detailed" {
			t.Errorf("%s: reasoning = %#v, want effort %s with summary detailed", tc.level, reasoning, tc.effort)
		}
		var want []types.Warning
		if string(tc.level) != tc.effort {
			want = []types.Warning{{Type: "compatibility", Feature: "reasoning",
				Details: `reasoning "` + string(tc.level) + `" is not directly supported by this model. mapped to effort "` + tc.effort + `".`}}
		}
		if !reflect.DeepEqual(warnings, want) {
			t.Errorf("%s: warnings = %#v, want %#v", tc.level, warnings, want)
		}
	}
}

// TS openai-chat-language-model.test.ts (c3ee424a70) o4-mini: reasoning none
// is not accepted, so the lowest supported effort is sent (the existing
// TestChatO4MiniStillClearsTemperatureWhenReasoningNone covers the warnings).
func TestPortprovbChatReasoningNoneMapsToSupportedEffort(t *testing.T) {
	none := types.ReasoningNone
	m := NewLanguageModel(New(Config{APIKey: "k"}), "o3")
	body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}, Reasoning: &none}, false)
	if err != nil {
		t.Fatal(err)
	}
	if body["reasoning_effort"] != "low" || len(warnings) != 1 || warnings[0].Type != "compatibility" {
		t.Fatalf("effort = %#v warnings = %#v", body["reasoning_effort"], warnings)
	}
}
