package cerebras

import "testing"

// TestCerebrasModelIDs verifies that all model ID constants have the expected values.
func TestCerebrasModelIDs(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"ModelGPTOSS120B", ModelGPTOSS120B, "gpt-oss-120b"},
		{"ModelGemma4_31B", ModelGemma4_31B, "gemma-4-31b"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// TestCerebrasModelIDsAcceptedByProvider verifies that model ID constants,
// and arbitrary/custom model IDs, are accepted by the provider without error
// (Cerebras does not validate model IDs; TS: (string & {})).
func TestCerebrasModelIDsAcceptedByProvider(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})

	modelIDs := []string{
		ModelGPTOSS120B,
		ModelGemma4_31B,
		"some-future-cerebras-model",
	}

	for _, id := range modelIDs {
		t.Run(id, func(t *testing.T) {
			_, err := prov.LanguageModel(id)
			if err != nil {
				t.Errorf("LanguageModel(%q) error = %v", id, err)
			}
		})
	}
}
