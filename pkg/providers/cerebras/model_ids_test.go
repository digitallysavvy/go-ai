package cerebras

import "testing"

// TestCerebrasModelIDs verifies that all model ID constants have the expected values.
func TestCerebrasModelIDs(t *testing.T) {
	cases := []struct {
		name  string
		got   string
		want  string
	}{
		{"ModelLlama31_8B", ModelLlama31_8B, "llama3.1-8b"},
		{"ModelGPTOSS120B", ModelGPTOSS120B, "gpt-oss-120b"},
		{"ModelQwen3_235BA22BInstruct2507", ModelQwen3_235BA22BInstruct2507, "qwen-3-235b-a22b-instruct-2507"},
		{"ModelQwen3_235BA22BThinking2507", ModelQwen3_235BA22BThinking2507, "qwen-3-235b-a22b-thinking-2507"},
		{"ModelZaiGLM4_6", ModelZaiGLM4_6, "zai-glm-4.6"},
		{"ModelZaiGLM4_7", ModelZaiGLM4_7, "zai-glm-4.7"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// TestCerebrasModelIDsAcceptedByProvider verifies that model ID constants are
// accepted by the provider without error.
func TestCerebrasModelIDsAcceptedByProvider(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})

	modelIDs := []string{
		ModelLlama31_8B,
		ModelGPTOSS120B,
		ModelQwen3_235BA22BInstruct2507,
		ModelQwen3_235BA22BThinking2507,
		ModelZaiGLM4_6,
		ModelZaiGLM4_7,
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
