// Package provider defines the interfaces that every model provider
// implements. Most applications do not import it directly: they create a
// concrete provider such as openai.New and pass its models to package ai. You
// need this package when you write a provider, wrap a model, or accept "any
// model" in your own function signature.
//
// The main types are:
//
//   - Provider: returns models by ID (LanguageModel, EmbeddingModel,
//     ImageModel, SpeechModel, TranscriptionModel, RerankingModel).
//   - LanguageModel: DoGenerate and DoStream, plus capability checks such as
//     SupportsTools.
//   - TextStream and StreamChunk: the stream a language model returns.
//   - EmbeddingModel, ImageModel, SpeechModel, TranscriptionModel,
//     RerankingModel, VideoModelV3: the other model kinds.
//   - Serialize* and Deserialize* functions: turn a model into JSON and back, so
//     a model can cross a durable workflow boundary.
//
// A function that works with any language model takes the interface:
//
//	func summarize(ctx context.Context, model provider.LanguageModel, text string) (string, error) {
//		res, err := ai.GenerateText(ctx, ai.GenerateTextOptions{Model: model, Prompt: "Summarize: " + text})
//		if err != nil {
//			return "", err
//		}
//		return res.Text, nil
//	}
//
// Package testutil has mock implementations of these interfaces for tests.
// Package registry builds providers from fixed maps of models.
//
// Reference: https://goaisdk.com/docs/reference/providers/language-model and
// https://goaisdk.com/docs/reference/providers/custom-provider.
//
// Serialization: this SDK does not call SerializeModel or DeserializeModel for
// you. In TypeScript the workflow runtime does this through the
// WORKFLOW_SERIALIZE and WORKFLOW_DESERIALIZE symbols. In Go, call the
// functions yourself before you persist a model across a step, queue message
// or other durability boundary, and call the matching Deserialize function
// after you load it. Every provider package registers its deserializers in an
// init function.
package provider
