package gladia

import (
	"strings"
	"testing"
)

// TS 6dd81a0519 catalog refactor (ruling 13): omitModel and singleLanguage
// came from exact-ID policies (solaria-1, solaria-3, default); any other ID
// has neither. Go keeps its documented round-88 S6 difference for IDs
// outside solaria-*: they are not sent (TS sends them), with a warning.
func TestPortProvcModelPoliciesOnUnknownIDs(t *testing.T) {
	cases := []struct {
		id            string
		wantModel     string
		wantModelWarn bool
	}{
		{"default", "", false},
		{"", "", false},
		{"solaria-1", "solaria-1", false},
		{"solaria-3", "solaria-3", false},
		{"solaria-4", "solaria-4", false}, // unknown solaria-*: sent, like TS
		{"future-model", "", true},        // S6: TS sends it; Go does not
	}
	for _, tc := range cases {
		m := &TranscriptionModel{provider: New(Config{APIKey: "k"}), modelID: tc.id}
		body, warnings := m.getArgs(nil)
		got, _ := body["model"].(string)
		warned := false
		single := false
		for _, w := range warnings {
			warned = warned || w.Feature == "modelId"
			single = single || strings.Contains(w.Message, "single language only")
		}
		if got != tc.wantModel || warned != tc.wantModelWarn {
			t.Errorf("model %q: sent %q warned %v, want %q %v", tc.id, got, warned, tc.wantModel, tc.wantModelWarn)
		}
		// singleLanguage is exactly solaria-3.
		if single != (tc.id == "solaria-3") {
			t.Errorf("model %q: single-language warning = %v", tc.id, single)
		}
	}
}
