package registry

import (
	"fmt"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/middleware"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// defaultSeparator is the default provider:model separator, matching
// TypeScript's createProviderRegistry default of ':'.
const defaultSeparator = ":"

// NoSuchProviderError mirrors the TypeScript AI SDK registry error. It is
// returned when a provider ID cannot be resolved, or when a provider does not
// expose Files or Skills (Reason is then set).
//
// As in TS (getProvider), ModelID is the provider ID that was looked up, and
// ModelType is the requested model type ("languageModel" for Files and
// Skills). TS NoSuchProviderError extends NoSuchModelError, so errors.As with
// a *providererrors.NoSuchModelError target also matches an unknown-provider
// error (but not a Reason error, which TS throws as a plain Error).
type NoSuchProviderError struct {
	ProviderID         string
	ModelID            string
	ModelType          string
	AvailableProviders []string
	Reason             string
}

func (e *NoSuchProviderError) Error() string {
	if e.Reason != "" {
		// TS files()/skills() throw a plain Error with exactly this text for a
		// registered provider that lacks the capability.
		return e.Reason
	}
	return fmt.Sprintf("No such provider: %s (available providers: %s)", e.ProviderID, strings.Join(e.AvailableProviders, ","))
}

// As lets errors.As match a *providererrors.NoSuchModelError target, as TS
// NoSuchModelError.isInstance(noSuchProviderError) is true. The capability
// errors (Reason set) are plain errors in TS and do not match.
func (e *NoSuchProviderError) As(target interface{}) bool {
	if e.Reason != "" {
		return false
	}
	t, ok := target.(**providererrors.NoSuchModelError)
	if !ok {
		return false
	}
	*t = &providererrors.NoSuchModelError{ModelID: e.ModelID, ModelType: e.ModelType, Message: e.Error()}
	return true
}

// noSuchProviderLocked builds the TS getProvider error: modelId is the provider ID.
func (r *Registry) noSuchProviderLocked(providerID, modelType string) *NoSuchProviderError {
	return &NoSuchProviderError{ProviderID: providerID, ModelID: providerID, ModelType: modelType, AvailableProviders: r.listProvidersLocked()}
}

// noSuchRegistryModel is the TS registry error when the provider has no
// factory for the model type or the factory returns no model: modelId is the
// whole registry ID ("provider:model").
func noSuchRegistryModel(id, modelType string) error {
	return providererrors.NewNoSuchModelError(id, modelType)
}

// Global registry instance
var globalRegistry = NewRegistry()

// Registry manages providers and model resolution
type Registry struct {
	mu        sync.RWMutex
	providers map[string]provider.Provider
	// providerOrder lists provider names in first-registration order, the
	// order TS Object.keys(providers) reports in NoSuchProviderError.
	providerOrder []string
	aliases       map[string]string // model alias -> provider:model
	tools         map[string]ToolEntry

	// separator is used between provider ID and model ID in a combined
	// "providerId<sep>modelId" identifier. Defaults to ":", matching
	// TypeScript's createProviderRegistry options.separator.
	separator string

	// languageModelMiddleware/imageModelMiddleware, when non-empty, are
	// applied (via middleware.WrapLanguageModel/WrapImageModel) to every
	// model resolved through this registry. Matches TypeScript's
	// createProviderRegistry options.languageModelMiddleware /
	// options.imageModelMiddleware.
	languageModelMiddleware []*middleware.LanguageModelMiddleware
	imageModelMiddleware    []*middleware.ImageModelMiddleware
}

// RegistryOption configures a Registry constructed via NewRegistry.
type RegistryOption func(*Registry)

// WithSeparator sets a custom separator between provider ID and model ID
// (e.g. "|" to resolve "openai|gpt-5"). Defaults to ":".
func WithSeparator(separator string) RegistryOption {
	return func(r *Registry) {
		r.separator = separator
	}
}

// WithLanguageModelMiddleware applies middleware to every language model
// resolved through the registry. When multiple middlewares are provided, the
// first middleware transforms the input first, and the last middleware wraps
// directly around the model (matching middleware.WrapLanguageModel).
func WithLanguageModelMiddleware(mw ...*middleware.LanguageModelMiddleware) RegistryOption {
	return func(r *Registry) {
		r.languageModelMiddleware = mw
	}
}

// WithImageModelMiddleware applies middleware to every image model resolved
// through the registry. When multiple middlewares are provided, the first
// middleware transforms the input first, and the last middleware wraps
// directly around the model (matching middleware.WrapImageModel).
func WithImageModelMiddleware(mw ...*middleware.ImageModelMiddleware) RegistryOption {
	return func(r *Registry) {
		r.imageModelMiddleware = mw
	}
}

// NewRegistry creates a new registry. By default the provider:model
// separator is ":" and no middleware is applied; pass RegistryOption values (e.g.
// WithSeparator, WithLanguageModelMiddleware, WithImageModelMiddleware) to
// configure it, mirroring TypeScript's createProviderRegistry(providers, options).
func NewRegistry(opts ...RegistryOption) *Registry {
	r := &Registry{
		providers: make(map[string]provider.Provider),
		aliases:   make(map[string]string),
		tools:     make(map[string]ToolEntry),
		separator: defaultSeparator,
	}
	for _, opt := range opts {
		opt(r)
	}
	if r.separator == "" {
		r.separator = defaultSeparator
	}
	return r
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
	if _, exists := r.providers[name]; !exists {
		r.providerOrder = append(r.providerOrder, name)
	}
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
	providerName, modelID, err := parseModelString(model, r.separator, "languageModel")
	if err != nil {
		return nil, err
	}

	// Get provider
	p, ok := r.providers[providerName]
	if !ok {
		return nil, r.noSuchProviderLocked(providerName, "languageModel")
	}

	// Get model from provider
	lm, err := p.LanguageModel(modelID)
	if err != nil {
		return nil, err
	}
	if lm == nil {
		return nil, noSuchRegistryModel(model, "languageModel")
	}

	// Apply any registry-configured language model middleware, matching
	// TypeScript's createProviderRegistry options.languageModelMiddleware.
	if len(r.languageModelMiddleware) > 0 {
		lm = middleware.WrapLanguageModel(lm, r.languageModelMiddleware, nil, nil)
	}

	return lm, nil
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
	providerName, modelID, err := parseModelString(model, r.separator, "embeddingModel")
	if err != nil {
		return nil, err
	}

	// Get provider
	p, ok := r.providers[providerName]
	if !ok {
		return nil, r.noSuchProviderLocked(providerName, "embeddingModel")
	}

	// Get model from provider
	em, err := p.EmbeddingModel(modelID)
	if err != nil {
		return nil, err
	}
	if em == nil {
		return nil, noSuchRegistryModel(model, "embeddingModel")
	}
	return em, nil
}

func (r *Registry) ResolveImageModel(model string) (provider.ImageModel, error) {
	p, id, modelID, err := r.resolveProviderAndModel(model, "imageModel")
	if err != nil {
		return nil, err
	}
	im, err := p.ImageModel(modelID)
	if err != nil {
		return nil, err
	}
	if im == nil {
		return nil, noSuchRegistryModel(id, "imageModel")
	}

	// Apply any registry-configured image model middleware, matching
	// TypeScript's createProviderRegistry options.imageModelMiddleware.
	// r.imageModelMiddleware is set only at construction (NewRegistry/
	// WithImageModelMiddleware) and never mutated afterward, so reading it
	// here without holding r.mu is safe.
	if len(r.imageModelMiddleware) > 0 {
		im = middleware.WrapImageModel(im, r.imageModelMiddleware, nil, nil)
	}

	return im, nil
}

func (r *Registry) ResolveSpeechModel(model string) (provider.SpeechModel, error) {
	p, id, modelID, err := r.resolveProviderAndModel(model, "speechModel")
	if err != nil {
		return nil, err
	}
	m, err := p.SpeechModel(modelID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, noSuchRegistryModel(id, "speechModel")
	}
	return m, nil
}

func (r *Registry) ResolveTranscriptionModel(model string) (provider.TranscriptionModel, error) {
	p, id, modelID, err := r.resolveProviderAndModel(model, "transcriptionModel")
	if err != nil {
		return nil, err
	}
	m, err := p.TranscriptionModel(modelID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, noSuchRegistryModel(id, "transcriptionModel")
	}
	return m, nil
}

func (r *Registry) ResolveRerankingModel(model string) (provider.RerankingModel, error) {
	p, id, modelID, err := r.resolveProviderAndModel(model, "rerankingModel")
	if err != nil {
		return nil, err
	}
	m, err := p.RerankingModel(modelID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, noSuchRegistryModel(id, "rerankingModel")
	}
	return m, nil
}

func (r *Registry) ResolveVideoModel(model string) (provider.VideoModelV3, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if target, ok := r.aliases[model]; ok {
		model = target
	}
	providerName, modelID, err := parseModelString(model, r.separator, "videoModel")
	if err != nil {
		return nil, err
	}
	p, ok := r.providers[providerName]
	if !ok {
		return nil, r.noSuchProviderLocked(providerName, "videoModel")
	}
	vp, ok := p.(interface {
		VideoModel(string) (provider.VideoModelV3, error)
	})
	if !ok {
		// TS: provider.videoModel?.(modelId) is undefined -> NoSuchModelError.
		return nil, noSuchRegistryModel(model, "videoModel")
	}
	vm, err := vp.VideoModel(modelID)
	if err != nil {
		return nil, err
	}
	if vm == nil {
		return nil, noSuchRegistryModel(model, "videoModel")
	}
	return vm, nil
}

// ResolveDecisionModel resolves a "provider:model" string to an
// DecisionModel. Decision is experimental and not part of the stable
// Provider contract: the resolved provider must additionally implement
// provider.DecisionModelProvider (mirrors TypeScript's registry
// `decisionModel`).
func (r *Registry) ResolveDecisionModel(model string) (provider.DecisionModel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if target, ok := r.aliases[model]; ok {
		model = target
	}
	providerName, modelID, err := parseModelString(model, r.separator, "decisionModel")
	if err != nil {
		return nil, err
	}
	p, ok := r.providers[providerName]
	if !ok {
		return nil, r.noSuchProviderLocked(providerName, "decisionModel")
	}
	resolved, ok, err := provider.DecisionModelFromProvider(p, modelID)
	if err != nil {
		return nil, err
	}
	if !ok || resolved == nil {
		// TS: no decisionModel/evaluationModel factory, or a factory that
		// returns no model -> NoSuchModelError({ modelId: id, modelType }).
		return nil, noSuchRegistryModel(model, "decisionModel")
	}
	return resolved, nil
}

func (r *Registry) Files(providerID string) (provider.FilesAPI, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[providerID]
	if !ok {
		// TS files()/skills() call getProvider(id, 'languageModel').
		return nil, r.noSuchProviderLocked(providerID, "languageModel")
	}
	api, err := provider.ResolveFilesAPI(p)
	if err != nil {
		return nil, &NoSuchProviderError{
			ProviderID:         providerID,
			ModelID:            providerID,
			ModelType:          "languageModel",
			AvailableProviders: r.listProvidersLocked(),
			Reason:             fmt.Sprintf("The provider %q does not support file uploads. Make sure it exposes a Files() method.", providerID),
		}
	}
	return api, nil
}

func (r *Registry) Skills(providerID string) (provider.SkillsAPI, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[providerID]
	if !ok {
		// TS files()/skills() call getProvider(id, 'languageModel').
		return nil, r.noSuchProviderLocked(providerID, "languageModel")
	}
	api, err := provider.ResolveSkillsAPI(p)
	if err != nil {
		return nil, &NoSuchProviderError{
			ProviderID:         providerID,
			ModelID:            providerID,
			ModelType:          "languageModel",
			AvailableProviders: r.listProvidersLocked(),
			Reason:             fmt.Sprintf("The provider %q does not support skills. Make sure it exposes a Skills() method.", providerID),
		}
	}
	return api, nil
}

// resolveProviderAndModel returns the provider, the full registry ID (after
// alias substitution) and the model part of it.
func (r *Registry) resolveProviderAndModel(model, modelType string) (provider.Provider, string, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if target, ok := r.aliases[model]; ok {
		model = target
	}
	providerName, modelID, err := parseModelString(model, r.separator, modelType)
	if err != nil {
		return nil, "", "", err
	}
	p, ok := r.providers[providerName]
	if !ok {
		return nil, "", "", r.noSuchProviderLocked(providerName, modelType)
	}
	return p, model, modelID, nil
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
	return append([]string(nil), r.providerOrder...)
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

// parseModelString parses a model string into provider and model ID using
// the given separator (defaults to ":" when empty). Formats supported:
//   - "provider<sep>model" -> ("provider", "model")
//   - "model" -> error if the separator is not present
//
// When the separator is missing, this returns a *providererrors.NoSuchModelError
// with the same message shape as TypeScript's DefaultProviderRegistry.splitId:
// `Invalid ${modelType} id for registry: ${id} (must be in the format
// "providerId${separator}modelId")` (provider-registry.ts).
func parseModelString(model, separator, modelType string) (providerID, modelID string, err error) {
	if separator == "" {
		separator = defaultSeparator
	}

	idx := strings.Index(model, separator)
	if idx == -1 {
		return "", "", &providererrors.NoSuchModelError{
			ModelID:   model,
			ModelType: modelType,
			Message: fmt.Sprintf(
				"Invalid %s id for registry: %s (must be in the format \"providerId%smodelId\")",
				modelType, model, separator,
			),
		}
	}

	return model[:idx], model[idx+len(separator):], nil
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

// ResolveDecisionModel resolves a decision model string using the
// global registry.
func ResolveDecisionModel(model string) (provider.DecisionModel, error) {
	return globalRegistry.ResolveDecisionModel(model)
}

// ResolveEvaluationModel is the deprecated form of ResolveDecisionModel.
//
// Deprecated: Use ResolveDecisionModel instead.
func (r *Registry) ResolveEvaluationModel(model string) (provider.EvaluationModel, error) {
	resolved, err := r.ResolveDecisionModel(model)
	if err != nil {
		return nil, err
	}
	return provider.AsEvaluationModel(resolved), nil
}

// ResolveEvaluationModel is the deprecated form of ResolveDecisionModel
// using the global registry.
//
// Deprecated: Use ResolveDecisionModel instead.
func ResolveEvaluationModel(model string) (provider.EvaluationModel, error) {
	return globalRegistry.ResolveEvaluationModel(model)
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
