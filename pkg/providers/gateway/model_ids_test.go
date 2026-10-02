package gateway

import (
	"strings"
	"testing"
)

// Expectations below mirror packages/gateway/src/gateway-*-model-settings.ts
// at ai@7.0.113. Regenerate with `go generate` (see generate.go) and update
// these counts when refreshing to a newer TS release.

func TestGatewayModelIDConstantsMatchSettings(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"language OpenAI 5.5", string(GatewayLanguageModelOpenaiGpt55), "openai/gpt-5.5"},
		{"language OpenAI 5.2", string(GatewayLanguageModelOpenaiGpt52), "openai/gpt-5.2"},
		{"language OpenAI GPT-6 Sol", string(GatewayLanguageModelOpenaiGpt6Sol), "openai/gpt-6-sol"},
		{"language Anthropic opus 4.8", string(GatewayLanguageModelAnthropicClaudeOpus48), "anthropic/claude-opus-4.8"},
		{"language Anthropic opus 5.5", string(GatewayLanguageModelAnthropicClaudeOpus55), "anthropic/claude-opus-5.5"},
		{"language Anthropic sonnet 5.5", string(GatewayLanguageModelAnthropicClaudeSonnet55), "anthropic/claude-sonnet-5.5"},
		{"language SpaceXAI grok 4.3", string(GatewayLanguageModelSpacexaiGrok43), "spacexai/grok-4.3"},
		{"language SpaceXAI grok 4.7", string(GatewayLanguageModelSpacexaiGrok47), "spacexai/grok-4.7"},
		{"language SpaceXAI grok build", string(GatewayLanguageModelSpacexaiGrokBuild01), "spacexai/grok-build-0.1"},
		{"language Alibaba qwen 3.7", string(GatewayLanguageModelAlibabaQwen37Max), "alibaba/qwen3.7-max"},
		{"language Moonshot code", string(GatewayLanguageModelMoonshotaiKimiK27Code), "moonshotai/kimi-k2.7-code"},
		{"language Mistral medium 3.5", string(GatewayLanguageModelMistralMistralMedium35), "mistral/mistral-medium-3.5"},
		{"language Xiaomi mimo 2.6 flash", string(GatewayLanguageModelXiaomiMimoV26Flash), "xiaomi/mimo-v2.6-flash"},
		{"language Zai 5.3 flashx", string(GatewayLanguageModelZaiGlm53Flashx), "zai/glm-5.3-flashx"},
		{"embedding Google", string(GatewayEmbeddingModelGoogleGeminiEmbedding2), "google/gemini-embedding-2"},
		{"embedding Perplexity", string(GatewayEmbeddingModelPerplexityPplxEmbedV106b), "perplexity/pplx-embed-v1-0.6b"},
		{"image OpenAI", string(GatewayImageModelOpenaiGptImage2), "openai/gpt-image-2"},
		{"image BFL", string(GatewayImageModelBflFlux2Pro), "bfl/flux-2-pro"},
		{"image SpaceXAI", string(GatewayImageModelSpacexaiGrokImagineImage), "spacexai/grok-imagine-image"},
		{"image SpaceXAI 2.0", string(GatewayImageModelSpacexaiGrokImagineImage20), "spacexai/grok-imagine-image-2.0"},
		{"video Kling", string(GatewayVideoModelKlingaiKlingV30T2v), "klingai/kling-v3.0-t2v"},
		{"video Alibaba", string(GatewayVideoModelAlibabaWanV26T2v), "alibaba/wan-v2.6-t2v"},
		{"video SpaceXAI", string(GatewayVideoModelSpacexaiGrokImagineVideo), "spacexai/grok-imagine-video"},
		{"video SpaceXAI 1.5", string(GatewayVideoModelSpacexaiGrokImagineVideo15), "spacexai/grok-imagine-video-1.5"},
		{"reranking Cohere", string(GatewayRerankingModelCohereRerankV4Pro), "cohere/rerank-v4-pro"},
		{"speech OpenAI", string(GatewaySpeechModelOpenaiTts1), "openai/tts-1"},
		{"speech SpaceXAI", string(GatewaySpeechModelSpacexaiGrokTts), "spacexai/grok-tts"},
		{"transcription OpenAI", string(GatewayTranscriptionModelOpenaiWhisper1), "openai/whisper-1"},
		{"transcription SpaceXAI", string(GatewayTranscriptionModelSpacexaiGrokStt), "spacexai/grok-stt"},
		{"realtime OpenAI", string(GatewayRealtimeModelOpenaiGptRealtime2), "openai/gpt-realtime-2"},
		{"realtime SpaceXAI", string(GatewayRealtimeModelSpacexaiGrokVoiceThinkFast20), "spacexai/grok-voice-think-fast-2.0"},
		{"evaluation", string(GatewayEvaluationModelTypesafeAiJev), "typesafe-ai/jev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("constant = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

// catalogs returns every kind's list as plain strings.
func catalogs() map[string][]string {
	out := map[string][]string{}
	add := func(kind string, n int, at func(int) string) {
		ids := make([]string, n)
		for i := range ids {
			ids[i] = at(i)
		}
		out[kind] = ids
	}
	add("language", len(GatewayLanguageModelIDs), func(i int) string { return string(GatewayLanguageModelIDs[i]) })
	add("embedding", len(GatewayEmbeddingModelIDs), func(i int) string { return string(GatewayEmbeddingModelIDs[i]) })
	add("image", len(GatewayImageModelIDs), func(i int) string { return string(GatewayImageModelIDs[i]) })
	add("video", len(GatewayVideoModelIDs), func(i int) string { return string(GatewayVideoModelIDs[i]) })
	add("reranking", len(GatewayRerankingModelIDs), func(i int) string { return string(GatewayRerankingModelIDs[i]) })
	add("speech", len(GatewaySpeechModelIDs), func(i int) string { return string(GatewaySpeechModelIDs[i]) })
	add("transcription", len(GatewayTranscriptionModelIDs), func(i int) string { return string(GatewayTranscriptionModelIDs[i]) })
	add("realtime", len(GatewayRealtimeModelIDs), func(i int) string { return string(GatewayRealtimeModelIDs[i]) })
	add("evaluation", len(GatewayEvaluationModelIDs), func(i int) string { return string(GatewayEvaluationModelIDs[i]) })
	return out
}

func TestGatewayModelIDCatalogsExposeAllModelKinds(t *testing.T) {
	want := map[string]int{
		"language":      263,
		"embedding":     26,
		"image":         33,
		"video":         35,
		"reranking":     5,
		"speech":        8,
		"transcription": 8,
		"realtime":      9,
		"evaluation":    1,
	}
	got := catalogs()
	for kind, n := range want {
		if len(got[kind]) != n {
			t.Errorf("%s catalog length = %d, want %d (TS ai@7.0.113)", kind, len(got[kind]), n)
		}
	}
}

func TestGatewayModelIDCatalogsAreUnique(t *testing.T) {
	for kind, ids := range catalogs() {
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				t.Errorf("%s catalog has duplicate %q", kind, id)
			}
			seen[id] = true
			if !strings.Contains(id, "/") {
				t.Errorf("%s catalog ID %q is not provider-qualified", kind, id)
			}
		}
	}
}

// The TS SDK renamed every xai/* gateway ID to spacexai/* (dedac59).
func TestGatewayModelIDCatalogsUseSpaceXAIPrefix(t *testing.T) {
	for kind, ids := range catalogs() {
		for _, id := range ids {
			if strings.HasPrefix(id, "xai/") {
				t.Errorf("%s catalog still contains legacy xAI ID %q", kind, id)
			}
		}
	}
}

func TestGatewayModelIDCatalogDropsRemovedModels(t *testing.T) {
	removed := map[string][]string{
		"language": {
			"anthropic/claude-3.5-haiku",
			"mistral/devstral-2",
			"mistral/pixtral-12b",
			"openai/gpt-5.2-chat",
			"xai/grok-4.3",
			"zai/glm-4.6v",
		},
		"image": {
			"google/imagen-4.0-generate-001",
			"xai/grok-imagine-image",
		},
		"video": {
			"bytedance/seedance-v1.0-lite-t2v",
			"xai/grok-imagine-video-1.5-preview",
		},
	}
	all := catalogs()
	for kind, ids := range removed {
		present := map[string]bool{}
		for _, id := range all[kind] {
			present[id] = true
		}
		for _, id := range ids {
			if present[id] {
				t.Errorf("%s catalog still contains removed ID %q", kind, id)
			}
		}
	}
}

func TestGatewayModelIDCatalogIncludesSep23Additions(t *testing.T) {
	want := map[string][]string{
		"language":      {"openai/gpt-6-luna", "spacexai/grok-4.7", "xiaomi/mimo-v2.6-pro", "mixedbread/toast-1", "inference-net/schematron-v2-small"},
		"embedding":     {"perplexity/pplx-embed-v1-0.6b", "perplexity/pplx-embed-v1-4b"},
		"image":         {"bytedance/seedream-5.0-pro", "quiverai/arrow-1.1", "spacexai/grok-imagine-image-2.0"},
		"video":         {"alibaba/wan-v3.0-video", "google/veo-3.1-lite-generate-001", "minimax/minimax-h3"},
		"speech":        {"fish-audio/s2.1-pro", "google/gemini-3.8-flash-tts"},
		"transcription": {"fish-audio/transcribe-1", "openai/gpt-realtime-whisper"},
		"realtime":      {"google/gemini-3.8-live", "openai/gpt-live-1"},
		"evaluation":    {"typesafe-ai/jev"},
	}
	all := catalogs()
	for kind, ids := range want {
		present := map[string]bool{}
		for _, id := range all[kind] {
			present[id] = true
		}
		for _, id := range ids {
			if !present[id] {
				t.Errorf("%s catalog missing %q", kind, id)
			}
		}
	}
}
