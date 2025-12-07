package registry

import (
	"fmt"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Global registry instance
var globalRegistry = NewRegistry()

// Registry manages providers and model resolution
type Registry struct {
	mu        sync.RWMutex
	providers map[string]provider.Provider
	aliases   map[string]string // model alias -> provider:model
}

// NewRegistry creates a new registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]provider.Provider),
		aliases:   make(map[string]string),
	}
}

// RegisterProvider registers a provider with a name
func (r *Registry) RegisterProvider(name string, p provider.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = p
}

// GetProvider returns a provider by name
func (r *Registry) GetProvider(name string) (provider.Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", name)
	}
	return p, nil
}

// RegisterAlias registers a model alias
// Example: RegisterAlias("gpt-4", "openai:gpt-4")
func (r *Registry) RegisterAlias(alias, target string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aliases[alias] = target
}

// ResolveLanguageModel resolves a model string to a LanguageModel
// Supports formats:
//   - "gpt-4" -> uses registered aliases
//   - "openai:gpt-4" -> provider:model format
func (r *Registry) ResolveLanguageModel(model string) (provider.LanguageModel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check if it's an alias
	if target, ok := r.aliases[model]; ok {
		model = target
	}

	// Parse provider:model format
	providerName, modelID, err := parseModelString(model)
	if err != nil {
		return nil, err
	}

	// Get provider
	p, ok := r.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", providerName)
	}

	// Get model from provider
	return p.LanguageModel(modelID)
}

// ResolveEmbeddingModel resolves a model string to an EmbeddingModel
func (r *Registry) ResolveEmbeddingModel(model string) (provider.EmbeddingModel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check if it's an alias
	if target, ok := r.aliases[model]; ok {
		model = target
	}

	// Parse provider:model format
	providerName, modelID, err := parseModelString(model)
	if err != nil {
		return nil, err
	}

	// Get provider
	p, ok := r.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", providerName)
	}

	// Get model from provider
	return p.EmbeddingModel(modelID)
}

// ListProviders returns all registered provider names
func (r *Registry) ListProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// ListAliases returns all registered aliases
func (r *Registry) ListAliases() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	aliases := make(map[string]string, len(r.aliases))
	for k, v := range r.aliases {
		aliases[k] = v
	}
	return aliases
}

// parseModelString parses a model string into provider and model ID
// Formats supported:
//   - "provider:model" -> ("provider", "model")
//   - "model" -> ("", "model") - error if no colon
func parseModelString(model string) (provider, modelID string, err error) {
	// Find colon separator
	for i := 0; i < len(model); i++ {
		if model[i] == ':' {
			return model[:i], model[i+1:], nil
		}
	}

	// No colon found
	return "", "", fmt.Errorf("invalid model string format (expected 'provider:model'): %s", model)
}

// Global registry functions

// RegisterProvider registers a provider in the global registry
func RegisterProvider(name string, p provider.Provider) {
	globalRegistry.RegisterProvider(name, p)
}

// GetProvider returns a provider from the global registry
func GetProvider(name string) (provider.Provider, error) {
	return globalRegistry.GetProvider(name)
}

// RegisterAlias registers a model alias in the global registry
func RegisterAlias(alias, target string) {
	globalRegistry.RegisterAlias(alias, target)
}

// ResolveLanguageModel resolves a model string using the global registry
func ResolveLanguageModel(model string) (provider.LanguageModel, error) {
	return globalRegistry.ResolveLanguageModel(model)
}

// ResolveEmbeddingModel resolves an embedding model string using the global registry
func ResolveEmbeddingModel(model string) (provider.EmbeddingModel, error) {
	return globalRegistry.ResolveEmbeddingModel(model)
}

// GetGlobalRegistry returns the global registry instance
func GetGlobalRegistry() *Registry {
	return globalRegistry
}
