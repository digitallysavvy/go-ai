package providers_test

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"

	// Blank-imported so every provider package's init() runs and registers
	// its deserializers with pkg/provider's typed registries -- this test
	// walks those registries directly (via the Registered*DeserializerKeys
	// accessors) rather than constructing each provider's models by hand,
	// so every provider needs to be imported here for its keys to show up.
	_ "github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/anthropicaws"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/assemblyai"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/azure"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/baseten"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/bfl"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/cartesia"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/cerebras"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/deepgram"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/elevenlabs"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/fal"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/fishaudio"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/gladia"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/google"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex/xai"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/groq"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/hume"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/luma"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/openai"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/prodia"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/quiverai"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/replicate"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/revai"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/together"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/typesafeai"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/voyage"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/xai"
	_ "github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// registryConfigOverrides supplies a non-default SerializedModel.Config for
// registry keys whose provider requires fields New() would otherwise reject
// (e.g. Google Vertex needs project/location, Vertex xAI needs a project).
// Every other key deserializes fine from an empty config (most New()
// functions fall back to env vars / zero values with no validation error).
var registryConfigOverrides = map[string]map[string]interface{}{
	// google-vertex's LanguageModel/EmbeddingModel/ImageModel: New() errors
	// without project+location+an access token unless an Express Mode API
	// key is set instead.
	"google-vertex": {"project": "test-project", "location": "us-central1", "accessToken": "test-token"},
	// google.vertex.transcription, google.vertex.speech and
	// google.vertex.interactions all unconditionally reject an Express Mode
	// API key for at least one of the model types they can route to (see
	// Provider.TranscriptionModel/Interactions, and the "chirp" branch of
	// Provider.SpeechModel, in pkg/providers/googlevertex/provider.go), so
	// only the project+location+accessToken form of auth works for every
	// googlevertex-tagged key; use it everywhere for consistency.
	"google.vertex.transcription": {"project": "test-project", "location": "us-central1", "accessToken": "test-token"},
	"google.vertex.speech":        {"project": "test-project", "location": "us-central1", "accessToken": "test-token"},
	"google.vertex.interactions":  {"project": "test-project", "location": "us-central1", "accessToken": "test-token"},
	// googleVertex.xai's New() requires a project unconditionally.
	"googleVertex.xai": {"Project": "test-project"},
	// open-responses.responses (and Open Responses generally) has no
	// default base URL -- Config.BaseURL is mandatory since there is no
	// single canonical Open Responses server, unlike every other provider
	// here (which defaults to a real API host).
	"open-responses.responses": {"baseURL": "https://example.test/v1"},
	// baseten.embedding restores via a plain openai.Config carrying the
	// already-resolved embeddings endpoint in BaseURL (see
	// deserializeEmbeddingModel in pkg/providers/baseten/serialization.go);
	// without it, Provider.EmbeddingModel has no endpoint to call.
	"baseten.embedding": {"BaseURL": "https://example.test/sync/v1"},
}

// registryModelIDOverrides supplies a non-default ModelID for registry keys
// whose provider validates the model ID against an allowlist instead of
// accepting an arbitrary string (most providers accept any non-empty ID and
// only reject wire-level errors at request time, which this test never
// reaches).
var registryModelIDOverrides = map[string]string{
	// Prodia only serves its Nano Banana image-editing model through
	// LanguageModel(); any other ID is rejected outright (see
	// Provider.LanguageModel in pkg/providers/prodia/provider.go).
	"prodia.language": "inference.nano-banana.img2img.v2",
}

func modelIDFor(key string) string {
	if id, ok := registryModelIDOverrides[key]; ok {
		return id
	}
	return "test-model"
}

// registryAliasKeys lists registered keys whose deserialized model's
// Provider() legitimately differs from the registry key it was registered
// under -- each is a documented alias/back-compat key, not a bug. For each,
// the test only asserts that deserialization succeeds; it does not require
// Provider() == key.
var registryAliasKeys = map[string]string{
	// "google" is a short alias for the default provider name
	// "google.generative-ai" (TS: providerName defaults to
	// 'google.generative-ai', options.name lets callers override it to
	// "google"; both tags are pre-registered here so either config
	// round-trips). A model deserialized under the bare "google" key was
	// never actually tagged that way unless its config set Name: "google";
	// with the default empty config used here it restores as
	// "google.generative-ai".
	"google": "restores as google.generative-ai with a default (Name-less) config",
	// "bedrock" is Provider.LanguageModel's historical/short tag; the
	// Amazon Bedrock Converse API model's own Provider() reports
	// "amazon-bedrock" (TS: `amazon-bedrock.messages` family -- see
	// pkg/providers/bedrock's Serialize()/Provider() for the exact string).
	"bedrock": "restores as amazon-bedrock (or amazon-bedrock.messages)",
	// "huggingface" registers the Responses-API language model, whose own
	// Provider() carries the ".responses" suffix.
	"huggingface": "restores as huggingface.responses",
	// "openai" is the bare alias for OpenAI's default LanguageModel(),
	// which -- since the Responses API migration -- routes to
	// ResponsesLanguageModel and reports "openai.responses".
	"openai": "restores as openai.responses",
	// "azure-openai" is the bare alias for Azure's default chat
	// LanguageModel(); azure.LanguageModel.Provider() reports "azure.chat"
	// (see azure/serialization.go's dedicated "azure.chat" key, registered
	// to the same deserializer).
	"azure-openai": "restores as azure.chat",
	// azure.completion and azure.responses both deserialize into a plain
	// *openai.CompletionModel / *openai.ResponsesLanguageModel (Azure has
	// no dedicated completion/responses model types -- see
	// deserializeCompletionModel/deserializeResponsesModel in
	// azure/serialization.go), so their Provider() reports the underlying
	// OpenAI type's tag rather than an "azure.*" string.
	"azure.completion": "restores as openai.completion",
	"azure.responses":  "restores as openai.responses",
	// "anthropic-aws.messages" is anthropicaws's registered key; it
	// deserializes into a plain *anthropic.LanguageModel (anthropicaws has
	// no dedicated model type -- see anthropicaws/serialization.go's doc
	// comment), whose Provider() returns anthropic.Config.Name. The
	// registered deserializer restores anthropic.Config verbatim, so
	// Provider() does equal the key in practice, but it is listed here
	// defensively since the mechanism (an aliased Name field, not a
	// dedicated type) matches the other alias entries above.
	"anthropic-aws.messages": "restores via anthropic.Config.Name override",
	// "google.interactions" is the bare alias for Google's own Interactions
	// API model, mirroring the plain "google" LanguageModel alias above:
	// with a default (Name-less) config it restores tagged
	// "google.generative-ai.interactions".
	"google.interactions": "restores as google.generative-ai.interactions with a default (Name-less) config",
	// "xai" is xAI's legacy Chat Completions tag; the Chat Completions
	// model was removed (row 1f20dba, see xai/serialization.go's doc
	// comment) and this key now restores the Responses API model instead,
	// whose Provider() carries the ".responses" suffix.
	"xai": "restores as xai.responses (legacy Chat Completions tag, model removed)",
	// "aws-bedrock" is Provider.LanguageModel's other historical/short tag
	// for Amazon Bedrock (alongside "bedrock" above); both restore the same
	// "amazon-bedrock"-tagged model.
	"aws-bedrock": "restores as amazon-bedrock (or amazon-bedrock.messages)",
	// "google.speech", "google.speech-translation" and "google.transcription"
	// are all bare aliases for Google's default (Name-less) provider name,
	// exactly like the plain "google"/"google.interactions" aliases above:
	// they restore tagged "google.generative-ai.<kind>".
	"google.speech":             "restores as google.generative-ai.speech with a default (Name-less) config",
	"google.speech-translation": "restores as google.generative-ai.speech-translation with a default (Name-less) config",
	"google.transcription":      "restores as google.generative-ai.transcription with a default (Name-less) config",
}

func configFor(key string) map[string]interface{} {
	if override, ok := registryConfigOverrides[key]; ok {
		out := make(map[string]interface{}, len(override))
		for k, v := range override {
			out[k] = v
		}
		return out
	}
	return map[string]interface{}{}
}

// TestModelDeserializerRegistryKeysRoundTrip walks every provider name
// registered with RegisterModelDeserializer (the LanguageModel registry)
// and asserts DeserializeModel succeeds for a minimal SerializedModel using
// that key, and that the restored model's Provider() matches the key --
// except for the documented aliases in registryAliasKeys.
func TestModelDeserializerRegistryKeysRoundTrip(t *testing.T) {
	keys := provider.RegisteredModelDeserializerKeys()
	if len(keys) == 0 {
		t.Fatal("no LanguageModel deserializers registered -- blank imports missing?")
	}
	for _, key := range keys {
		key := key
		t.Run(key, func(t *testing.T) {
			restored, err := provider.DeserializeModel(provider.SerializedModel{
				Provider: key,
				ModelID:  modelIDFor(key),
				Config:   configFor(key),
			})
			if err != nil {
				t.Fatalf("DeserializeModel(%q) error = %v", key, err)
			}
			if want := modelIDFor(key); restored.ModelID() != want {
				t.Fatalf("restored ModelID = %q, want %q", restored.ModelID(), want)
			}
			if reason, ok := registryAliasKeys[key]; ok {
				t.Logf("alias key %q: %s (restored Provider() = %q)", key, reason, restored.Provider())
				return
			}
			if restored.Provider() != key {
				t.Fatalf("restored Provider() = %q, want %q (registry key); if this is a legitimate alias, add it to registryAliasKeys with a comment", restored.Provider(), key)
			}
		})
	}
}

func TestImageModelDeserializerRegistryKeysRoundTrip(t *testing.T) {
	keys := provider.RegisteredImageModelDeserializerKeys()
	if len(keys) == 0 {
		t.Fatal("no ImageModel deserializers registered -- blank imports missing?")
	}
	for _, key := range keys {
		key := key
		t.Run(key, func(t *testing.T) {
			restored, err := provider.DeserializeImageModel(provider.SerializedModel{
				Provider: key,
				ModelID:  modelIDFor(key),
				Config:   configFor(key),
			})
			if err != nil {
				t.Fatalf("DeserializeImageModel(%q) error = %v", key, err)
			}
			if reason, ok := registryAliasKeys[key]; ok {
				t.Logf("alias key %q: %s (restored Provider() = %q)", key, reason, restored.Provider())
				return
			}
			if restored.Provider() != key {
				t.Fatalf("restored Provider() = %q, want %q", restored.Provider(), key)
			}
		})
	}
}

func TestVideoModelDeserializerRegistryKeysRoundTrip(t *testing.T) {
	keys := provider.RegisteredVideoModelDeserializerKeys()
	for _, key := range keys {
		key := key
		t.Run(key, func(t *testing.T) {
			restored, err := provider.DeserializeVideoModel(provider.SerializedModel{
				Provider: key,
				ModelID:  modelIDFor(key),
				Config:   configFor(key),
			})
			if err != nil {
				t.Fatalf("DeserializeVideoModel(%q) error = %v", key, err)
			}
			if reason, ok := registryAliasKeys[key]; ok {
				t.Logf("alias key %q: %s (restored Provider() = %q)", key, reason, restored.Provider())
				return
			}
			if restored.Provider() != key {
				t.Fatalf("restored Provider() = %q, want %q", restored.Provider(), key)
			}
		})
	}
}

func TestSpeechModelDeserializerRegistryKeysRoundTrip(t *testing.T) {
	keys := provider.RegisteredSpeechModelDeserializerKeys()
	if len(keys) == 0 {
		t.Fatal("no SpeechModel deserializers registered -- blank imports missing?")
	}
	for _, key := range keys {
		key := key
		t.Run(key, func(t *testing.T) {
			restored, err := provider.DeserializeSpeechModel(provider.SerializedModel{
				Provider: key,
				ModelID:  modelIDFor(key),
				Config:   configFor(key),
			})
			if err != nil {
				t.Fatalf("DeserializeSpeechModel(%q) error = %v", key, err)
			}
			if reason, ok := registryAliasKeys[key]; ok {
				t.Logf("alias key %q: %s (restored Provider() = %q)", key, reason, restored.Provider())
				return
			}
			if restored.Provider() != key {
				t.Fatalf("restored Provider() = %q, want %q", restored.Provider(), key)
			}
		})
	}
}

func TestSpeechTranslationModelDeserializerRegistryKeysRoundTrip(t *testing.T) {
	keys := provider.RegisteredSpeechTranslationModelDeserializerKeys()
	if len(keys) == 0 {
		t.Fatal("no SpeechTranslationModel deserializers registered -- blank imports missing?")
	}
	for _, key := range keys {
		key := key
		t.Run(key, func(t *testing.T) {
			restored, err := provider.DeserializeSpeechTranslationModel(provider.SerializedModel{
				Provider: key,
				ModelID:  modelIDFor(key),
				Config:   configFor(key),
			})
			if err != nil {
				t.Fatalf("DeserializeSpeechTranslationModel(%q) error = %v", key, err)
			}
			if reason, ok := registryAliasKeys[key]; ok {
				t.Logf("alias key %q: %s (restored Provider() = %q)", key, reason, restored.Provider())
				return
			}
			if restored.Provider() != key {
				t.Fatalf("restored Provider() = %q, want %q", restored.Provider(), key)
			}
		})
	}
}

func TestTranscriptionModelDeserializerRegistryKeysRoundTrip(t *testing.T) {
	keys := provider.RegisteredTranscriptionModelDeserializerKeys()
	if len(keys) == 0 {
		t.Fatal("no TranscriptionModel deserializers registered -- blank imports missing?")
	}
	for _, key := range keys {
		key := key
		t.Run(key, func(t *testing.T) {
			restored, err := provider.DeserializeTranscriptionModel(provider.SerializedModel{
				Provider: key,
				ModelID:  modelIDFor(key),
				Config:   configFor(key),
			})
			if err != nil {
				t.Fatalf("DeserializeTranscriptionModel(%q) error = %v", key, err)
			}
			if reason, ok := registryAliasKeys[key]; ok {
				t.Logf("alias key %q: %s (restored Provider() = %q)", key, reason, restored.Provider())
				return
			}
			if restored.Provider() != key {
				t.Fatalf("restored Provider() = %q, want %q", restored.Provider(), key)
			}
		})
	}
}

func TestEmbeddingModelDeserializerRegistryKeysRoundTrip(t *testing.T) {
	keys := provider.RegisteredEmbeddingModelDeserializerKeys()
	if len(keys) == 0 {
		t.Fatal("no EmbeddingModel deserializers registered -- blank imports missing?")
	}
	for _, key := range keys {
		key := key
		t.Run(key, func(t *testing.T) {
			restored, err := provider.DeserializeEmbeddingModel(provider.SerializedModel{
				Provider: key,
				ModelID:  modelIDFor(key),
				Config:   configFor(key),
			})
			if err != nil {
				t.Fatalf("DeserializeEmbeddingModel(%q) error = %v", key, err)
			}
			if reason, ok := registryAliasKeys[key]; ok {
				t.Logf("alias key %q: %s (restored Provider() = %q)", key, reason, restored.Provider())
				return
			}
			if restored.Provider() != key {
				t.Fatalf("restored Provider() = %q, want %q", restored.Provider(), key)
			}
		})
	}
}

func TestEvaluationModelDeserializerRegistryKeysRoundTrip(t *testing.T) {
	keys := provider.RegisteredEvaluationModelDeserializerKeys()
	for _, key := range keys {
		key := key
		t.Run(key, func(t *testing.T) {
			restored, err := provider.DeserializeEvaluationModel(provider.SerializedModel{
				Provider: key,
				ModelID:  modelIDFor(key),
				Config:   configFor(key),
			})
			if err != nil {
				t.Fatalf("DeserializeEvaluationModel(%q) error = %v", key, err)
			}
			if reason, ok := registryAliasKeys[key]; ok {
				t.Logf("alias key %q: %s (restored Provider() = %q)", key, reason, restored.Provider())
				return
			}
			if restored.Provider() != key {
				t.Fatalf("restored Provider() = %q, want %q", restored.Provider(), key)
			}
		})
	}
}
