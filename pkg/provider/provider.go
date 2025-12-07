package provider

// Provider represents an AI provider (OpenAI, Anthropic, Google, etc.)
// Each provider implementation should return models that implement
// the appropriate model interfaces (LanguageModel, EmbeddingModel, etc.)
type Provider interface {
	// Name returns the provider name for logging and telemetry
	Name() string

	// LanguageModel returns a language model by ID
	// Returns an error if the model ID is not supported
	LanguageModel(modelID string) (LanguageModel, error)

	// EmbeddingModel returns an embedding model by ID
	// Returns an error if the model ID is not supported
	EmbeddingModel(modelID string) (EmbeddingModel, error)

	// ImageModel returns an image generation model by ID (optional)
	// Returns an error if the model ID is not supported or not implemented
	ImageModel(modelID string) (ImageModel, error)

	// SpeechModel returns a speech synthesis model by ID (optional)
	// Returns an error if the model ID is not supported or not implemented
	SpeechModel(modelID string) (SpeechModel, error)

	// TranscriptionModel returns a speech-to-text model by ID (optional)
	// Returns an error if the model ID is not supported or not implemented
	TranscriptionModel(modelID string) (TranscriptionModel, error)

	// RerankingModel returns a reranking model by ID (optional)
	// Returns an error if the model ID is not supported or not implemented
	RerankingModel(modelID string) (RerankingModel, error)
}
