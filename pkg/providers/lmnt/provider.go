// Package lmnt provides an LMNT text-to-speech provider for the Go AI SDK.
// LMNT offers high-quality voice synthesis with low latency and natural-sounding voices.
package lmnt

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Config holds configuration for the LMNT provider
type Config struct {
	// API key for authentication
	APIKey string

	// Base URL for the LMNT API (optional, defaults to production)
	BaseURL string
}

// Provider represents the LMNT provider
type Provider struct {
	config Config
}

// New creates a new LMNT provider instance
func New(config Config) *Provider {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.lmnt.com/v1"
	}

	return &Provider{
		config: config,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "lmnt"
}

// LanguageModel returns a language model (not supported by LMNT)
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("lmnt does not provide language models")
}

// EmbeddingModel returns an embedding model (not supported by LMNT)
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("lmnt does not provide embedding models")
}

// ImageModel returns an image model (not supported by LMNT)
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("lmnt does not provide image models")
}

// SpeechModel returns a text-to-speech model
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return &SpeechModel{
		provider: p,
		modelID:  modelID,
	}, nil
}

// TranscriptionModel returns a speech-to-text model (not supported by LMNT)
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("lmnt does not provide transcription models")
}

// RerankingModel returns a reranking model (not supported by LMNT)
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("lmnt does not provide reranking models")
}
