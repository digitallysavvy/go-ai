package googlevertex

import "testing"

// TestModelConstants_Sep23_2026Additions covers the Sep 23 2026 parity model
// ID additions: gemini-3.5..3.8 flash, legacy embedding models, Chirp/telephony
// speech+transcription, and Veo (google-vertex-embedding-model-options.ts,
// google-vertex-speech-model-options.ts, google-vertex-transcription-model-options.ts,
// google-vertex-video-settings.ts).
func TestModelConstants_Sep23_2026Additions(t *testing.T) {
	cases := map[string]string{
		ModelGemini35Flash:     "gemini-3.5-flash",
		ModelGemini35FlashLite: "gemini-3.5-flash-lite",
		ModelGemini36Flash:     "gemini-3.6-flash",
		ModelGemini37Flash:     "gemini-3.7-flash",
		ModelGemini38Flash:     "gemini-3.8-flash",

		EmbeddingModelTextEmbeddingGecko:                "textembedding-gecko",
		EmbeddingModelTextEmbeddingGecko001:             "textembedding-gecko@001",
		EmbeddingModelTextEmbeddingGecko003:             "textembedding-gecko@003",
		EmbeddingModelTextEmbeddingGeckoMultilingual:    "textembedding-gecko-multilingual",
		EmbeddingModelTextEmbeddingGeckoMultilingual001: "textembedding-gecko-multilingual@001",
		EmbeddingModelTextMultilingualEmbedding002:      "text-multilingual-embedding-002",

		SpeechModelChirp3HD:         "chirp-3-hd",
		TranscriptionModelChirp3:    "chirp_3",
		TranscriptionModelTelephony: "telephony",

		ModelVeo20GeneratePreview:     "veo-2.0-generate-preview",
		ModelVeo20GenerateExp:         "veo-2.0-generate-exp",
		ModelVeo20Generate001:         "veo-2.0-generate-001",
		ModelVeo30Generate001:         "veo-3.0-generate-001",
		ModelVeo30FastGenerate001:     "veo-3.0-fast-generate-001",
		ModelVeo30GeneratePreview:     "veo-3.0-generate-preview",
		ModelVeo30FastGeneratePreview: "veo-3.0-fast-generate-preview",
		ModelVeo31Generate001:         "veo-3.1-generate-001",
		ModelVeo31FastGenerate001:     "veo-3.1-fast-generate-001",
		ModelVeo31GeneratePreview:     "veo-3.1-generate-preview",
		ModelVeo31FastGeneratePreview: "veo-3.1-fast-generate-preview",
	}
	for constant, want := range cases {
		if constant != want {
			t.Errorf("constant = %q, want %q", constant, want)
		}
	}
}
