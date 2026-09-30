package groq

import "testing"

// TestGroqModelIDs verifies that all model ID constants have the expected
// values, matching ai/packages/groq/src/groq-chat-language-model-options.ts
// (GroqChatModelId) and groq-transcription-model-options.ts
// (GroqTranscriptionModelId).
func TestGroqModelIDs(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		// production models
		{"ModelGemma29BIt", ModelGemma29BIt, "gemma2-9b-it"},
		{"ModelLlama318BInstant", ModelLlama318BInstant, "llama-3.1-8b-instant"},
		{"ModelLlama3370BVersatile", ModelLlama3370BVersatile, "llama-3.3-70b-versatile"},
		{"ModelMetaLlamaGuard412B", ModelMetaLlamaGuard412B, "meta-llama/llama-guard-4-12b"},
		{"ModelOpenAIGptOss120B", ModelOpenAIGptOss120B, "openai/gpt-oss-120b"},
		{"ModelOpenAIGptOss20B", ModelOpenAIGptOss20B, "openai/gpt-oss-20b"},
		// preview models (selection)
		{"ModelDeepseekR1DistillLlama70B", ModelDeepseekR1DistillLlama70B, "deepseek-r1-distill-llama-70b"},
		{"ModelMetaLlama4Maverick17B128EInstruct", ModelMetaLlama4Maverick17B128EInstruct, "meta-llama/llama-4-maverick-17b-128e-instruct"},
		{"ModelMetaLlama4Scout17B16EInstruct", ModelMetaLlama4Scout17B16EInstruct, "meta-llama/llama-4-scout-17b-16e-instruct"},
		{"ModelMetaLlamaPromptGuard222M", ModelMetaLlamaPromptGuard222M, "meta-llama/llama-prompt-guard-2-22m"},
		{"ModelMetaLlamaPromptGuard286M", ModelMetaLlamaPromptGuard286M, "meta-llama/llama-prompt-guard-2-86m"},
		{"ModelMoonshotAIKimiK2Instruct0905", ModelMoonshotAIKimiK2Instruct0905, "moonshotai/kimi-k2-instruct-0905"},
		{"ModelQwenQwen3627B", ModelQwenQwen3627B, "qwen/qwen3.6-27b"},
		{"ModelLlamaGuard38B", ModelLlamaGuard38B, "llama-guard-3-8b"},
		{"ModelLlama370B8192", ModelLlama370B8192, "llama3-70b-8192"},
		{"ModelLlama38B8192", ModelLlama38B8192, "llama3-8b-8192"},
		{"ModelMixtral8X7B32768", ModelMixtral8X7B32768, "mixtral-8x7b-32768"},
		{"ModelQwenQwq32B", ModelQwenQwq32B, "qwen-qwq-32b"},
		{"ModelQwen2532B", ModelQwen2532B, "qwen-2.5-32b"},
		{"ModelDeepseekR1DistillQwen32B", ModelDeepseekR1DistillQwen32B, "deepseek-r1-distill-qwen-32b"},
		// transcription models
		{"ModelWhisperLargeV3Turbo", ModelWhisperLargeV3Turbo, "whisper-large-v3-turbo"},
		{"ModelWhisperLargeV3", ModelWhisperLargeV3, "whisper-large-v3"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// TestGroqModelIDsAcceptedByProvider verifies that model ID constants, and
// arbitrary/custom model IDs, are accepted by the provider without error
// (Groq's chat model catalog is open-ended; TS: `| (string & {})`).
func TestGroqModelIDsAcceptedByProvider(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})

	modelIDs := []string{
		ModelGemma29BIt,
		ModelLlama318BInstant,
		ModelLlama3370BVersatile,
		ModelOpenAIGptOss120B,
		ModelMoonshotAIKimiK2Instruct0905,
		"some-future-groq-model",
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
