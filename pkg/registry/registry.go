package registry

import (
	"fmt"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// NoSuchProviderError mirrors the TypeScript AI SDK registry error. It is
// returned when a provider ID cannot be resolved, or when a provider does not
// expose a requested capability such as Files or Skills.
type NoSuchProviderError struct {
	ProviderID         string
	ModelID            string
	ModelType          string
	AvailableProviders []string
	Reason             string
}

func (e *NoSuchProviderError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("No such provider: %s (%s; available providers: %s)", e.ProviderID, e.Reason, strings.Join(e.AvailableProviders, ","))
	}
	return fmt.Sprintf("No such provider: %s (available providers: %s)", e.ProviderID, strings.Join(e.AvailableProviders, ","))
}

// Global registry instance
var globalRegistry = NewRegistry()

// Registry manages providers and model resolution
type Registry struct {
	mu        sync.RWMutex
	providers map[string]provider.Provider
	aliases   map[string]string // model alias -> provider:model
	tools     map[string]ToolEntry
}

// NewRegistry creates a new registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]provider.Provider),
		aliases:   make(map[string]string),
		tools:     make(map[string]ToolEntry),
	}
}

const (
	ToolKindLocal           = "local"
	ToolKindProviderDefined = "provider-defined"
)

// ToolEntry describes a registry tool and preserves provider-defined metadata.
type ToolEntry struct {
	Name             string
	Kind             string
	ProviderName     string
	ProviderMetadata map[string]interface{}
	Factory          func() types.Tool
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
		return nil, &NoSuchProviderError{ProviderID: name, AvailableProviders: r.listProvidersLocked()}
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
		return nil, &NoSuchProviderError{ProviderID: providerName, ModelID: modelID, ModelType: "languageModel", AvailableProviders: r.listProvidersLocked()}
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
		return nil, &NoSuchProviderError{ProviderID: providerName, ModelID: modelID, ModelType: "embeddingModel", AvailableProviders: r.listProvidersLocked()}
	}

	// Get model from provider
	return p.EmbeddingModel(modelID)
}

func (r *Registry) ResolveImageModel(model string) (provider.ImageModel, error) {
	p, modelID, err := r.resolveProviderAndModel(model, "imageModel")
	if err != nil {
		return nil, err
	}
	return p.ImageModel(modelID)
}

func (r *Registry) ResolveSpeechModel(model string) (provider.SpeechModel, error) {
	p, modelID, err := r.resolveProviderAndModel(model, "speechModel")
	if err != nil {
		return nil, err
	}
	return p.SpeechModel(modelID)
}

func (r *Registry) ResolveTranscriptionModel(model string) (provider.TranscriptionModel, error) {
	p, modelID, err := r.resolveProviderAndModel(model, "transcriptionModel")
	if err != nil {
		return nil, err
	}
	return p.TranscriptionModel(modelID)
}

func (r *Registry) ResolveRerankingModel(model string) (provider.RerankingModel, error) {
	p, modelID, err := r.resolveProviderAndModel(model, "rerankingModel")
	if err != nil {
		return nil, err
	}
	return p.RerankingModel(modelID)
}

func (r *Registry) ResolveVideoModel(model string) (provider.VideoModelV3, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if target, ok := r.aliases[model]; ok {
		model = target
	}
	providerName, modelID, err := parseModelString(model)
	if err != nil {
		return nil, err
	}
	p, ok := r.providers[providerName]
	if !ok {
		return nil, &NoSuchProviderError{ProviderID: providerName, ModelID: modelID, ModelType: "videoModel", AvailableProviders: r.listProvidersLocked()}
	}
	vp, ok := p.(interface {
		VideoModel(string) (provider.VideoModelV3, error)
	})
	if !ok {
		return nil, &NoSuchProviderError{ProviderID: providerName, ModelID: modelID, ModelType: "videoModel", AvailableProviders: r.listProvidersLocked()}
	}
	return vp.VideoModel(modelID)
}

func (r *Registry) Files(providerID string) (provider.FilesAPI, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[providerID]
	if !ok {
		return nil, &NoSuchProviderError{ProviderID: providerID, ModelType: "files", AvailableProviders: r.listProvidersLocked()}
	}
	api, err := provider.ResolveFilesAPI(p)
	if err != nil {
		return nil, fmt.Errorf("the provider %q does not support file uploads. Make sure it exposes a Files() method", providerID)
	}
	return api, nil
}

func (r *Registry) Skills(providerID string) (provider.SkillsAPI, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[providerID]
	if !ok {
		return nil, &NoSuchProviderError{ProviderID: providerID, ModelType: "skills", AvailableProviders: r.listProvidersLocked()}
	}
	api, err := provider.ResolveSkillsAPI(p)
	if err != nil {
		return nil, fmt.Errorf("the provider %q does not support skills. Make sure it exposes a Skills() method", providerID)
	}
	return api, nil
}

func (r *Registry) resolveProviderAndModel(model, modelType string) (provider.Provider, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if target, ok := r.aliases[model]; ok {
		model = target
	}
	providerName, modelID, err := parseModelString(model)
	if err != nil {
		return nil, "", err
	}
	p, ok := r.providers[providerName]
	if !ok {
		return nil, "", &NoSuchProviderError{ProviderID: providerName, ModelID: modelID, ModelType: modelType, AvailableProviders: r.listProvidersLocked()}
	}
	return p, modelID, nil
}

// ResolveFilesAPI returns the files upload API for a registered provider.
func (r *Registry) ResolveFilesAPI(providerName string) (provider.FilesAPI, error) {
	return r.Files(providerName)
}

// ResolveSkillsAPI returns the skills upload API for a registered provider.
func (r *Registry) ResolveSkillsAPI(providerName string) (provider.SkillsAPI, error) {
	return r.Skills(providerName)
}

// ListProviders returns all registered provider names
func (r *Registry) ListProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listProvidersLocked()
}

func (r *Registry) listProvidersLocked() []string {
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

// RegisterTool registers a tool factory and its metadata.
func (r *Registry) RegisterTool(entry ToolEntry) error {
	if entry.Name == "" {
		return fmt.Errorf("tool entry name is required")
	}
	if entry.Kind == "" {
		entry.Kind = ToolKindLocal
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[entry.Name] = cloneToolEntry(entry)
	return nil
}

// LookupTool returns a registered tool entry by name.
func (r *Registry) LookupTool(name string) (ToolEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.tools[name]
	if !ok {
		return ToolEntry{}, fmt.Errorf("tool not found: %s", name)
	}
	return cloneToolEntry(entry), nil
}

// ListTools returns all registered tool entries keyed by name.
func (r *Registry) ListTools() map[string]ToolEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make(map[string]ToolEntry, len(r.tools))
	for name, entry := range r.tools {
		tools[name] = cloneToolEntry(entry)
	}
	return tools
}

func cloneToolEntry(entry ToolEntry) ToolEntry {
	entry.ProviderMetadata = cloneMap(entry.ProviderMetadata)
	return entry
}

func cloneMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		if nested, ok := v.(map[string]interface{}); ok {
			out[k] = cloneMap(nested)
			continue
		}
		out[k] = v
	}
	return out
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

func ResolveImageModel(model string) (provider.ImageModel, error) {
	return globalRegistry.ResolveImageModel(model)
}

func ResolveSpeechModel(model string) (provider.SpeechModel, error) {
	return globalRegistry.ResolveSpeechModel(model)
}

func ResolveTranscriptionModel(model string) (provider.TranscriptionModel, error) {
	return globalRegistry.ResolveTranscriptionModel(model)
}

func ResolveRerankingModel(model string) (provider.RerankingModel, error) {
	return globalRegistry.ResolveRerankingModel(model)
}

func ResolveVideoModel(model string) (provider.VideoModelV3, error) {
	return globalRegistry.ResolveVideoModel(model)
}

func Files(providerID string) (provider.FilesAPI, error) {
	return globalRegistry.Files(providerID)
}

func Skills(providerID string) (provider.SkillsAPI, error) {
	return globalRegistry.Skills(providerID)
}

// ResolveFilesAPI resolves a provider files API using the global registry.
func ResolveFilesAPI(providerName string) (provider.FilesAPI, error) {
	return globalRegistry.ResolveFilesAPI(providerName)
}

// ResolveSkillsAPI resolves a provider skills API using the global registry.
func ResolveSkillsAPI(providerName string) (provider.SkillsAPI, error) {
	return globalRegistry.ResolveSkillsAPI(providerName)
}

// RegisterTool registers a tool in the global registry.
func RegisterTool(entry ToolEntry) error {
	return globalRegistry.RegisterTool(entry)
}

// LookupTool returns a tool entry from the global registry.
func LookupTool(name string) (ToolEntry, error) {
	return globalRegistry.LookupTool(name)
}

// ListTools returns all tool entries from the global registry.
func ListTools() map[string]ToolEntry {
	return globalRegistry.ListTools()
}

// GetGlobalRegistry returns the global registry instance
func GetGlobalRegistry() *Registry {
	return globalRegistry
}
