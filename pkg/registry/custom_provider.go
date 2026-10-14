package registry

import (
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// CustomProviderOptions configures an idiomatic Go custom provider. Each model
// family and upload API can be exposed independently.
type CustomProviderOptions struct {
	Name string

	LanguageModels      map[string]provider.LanguageModel
	EmbeddingModels     map[string]provider.EmbeddingModel
	ImageModels         map[string]provider.ImageModel
	SpeechModels        map[string]provider.SpeechModel
	TranscriptionModels map[string]provider.TranscriptionModel
	RerankingModels     map[string]provider.RerankingModel
	VideoModels         map[string]provider.VideoModelV3

	// DecisionModels maps model ID -> decision model. Decision is
	// experimental and not part of the stable Provider contract, mirroring
	// TypeScript's customProvider({ decisionModels }).
	DecisionModels map[string]provider.DecisionModel

	// EvaluationModels is the pre-rename form of DecisionModels. When
	// DecisionModels is set it takes precedence.
	//
	// Deprecated: Use DecisionModels instead.
	EvaluationModels map[string]provider.EvaluationModel //nolint:staticcheck // deprecated compatibility field

	FilesFactory  func() provider.FilesAPI
	SkillsFactory func() provider.SkillsAPI

	Fallback provider.Provider
}

// NewCustomProvider creates a provider backed by explicit model/API maps.
func NewCustomProvider(opts CustomProviderOptions) provider.Provider {
	base := &customProvider{opts: opts}
	hasFiles := opts.FilesFactory != nil
	if !hasFiles && opts.Fallback != nil {
		_, hasFiles = opts.Fallback.(provider.FilesProvider)
	}
	hasSkills := opts.SkillsFactory != nil
	if !hasSkills && opts.Fallback != nil {
		_, hasSkills = opts.Fallback.(provider.SkillsProvider)
	}
	switch {
	case hasFiles && hasSkills:
		return &customProviderWithFilesAndSkills{customProvider: base}
	case hasFiles:
		return &customProviderWithFiles{customProvider: base}
	case hasSkills:
		return &customProviderWithSkills{customProvider: base}
	default:
		return base
	}
}

type customProvider struct {
	opts CustomProviderOptions
}

func (p *customProvider) Name() string {
	if p.opts.Name != "" {
		return p.opts.Name
	}
	if p.opts.Fallback != nil {
		return p.opts.Fallback.Name()
	}
	return "custom"
}

func (p *customProvider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if model := p.opts.LanguageModels[modelID]; model != nil {
		return model, nil
	}
	if p.opts.Fallback != nil {
		return p.opts.Fallback.LanguageModel(modelID)
	}
	return nil, noSuchModel(modelID, "languageModel")
}

func (p *customProvider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if model := p.opts.EmbeddingModels[modelID]; model != nil {
		return model, nil
	}
	if p.opts.Fallback != nil {
		return p.opts.Fallback.EmbeddingModel(modelID)
	}
	return nil, noSuchModel(modelID, "embeddingModel")
}

func (p *customProvider) ImageModel(modelID string) (provider.ImageModel, error) {
	if model := p.opts.ImageModels[modelID]; model != nil {
		return model, nil
	}
	if p.opts.Fallback != nil {
		return p.opts.Fallback.ImageModel(modelID)
	}
	return nil, noSuchModel(modelID, "imageModel")
}

func (p *customProvider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	if model := p.opts.SpeechModels[modelID]; model != nil {
		return model, nil
	}
	if p.opts.Fallback != nil {
		return p.opts.Fallback.SpeechModel(modelID)
	}
	return nil, noSuchModel(modelID, "speechModel")
}

func (p *customProvider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	if model := p.opts.TranscriptionModels[modelID]; model != nil {
		return model, nil
	}
	if p.opts.Fallback != nil {
		return p.opts.Fallback.TranscriptionModel(modelID)
	}
	return nil, noSuchModel(modelID, "transcriptionModel")
}

func (p *customProvider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	if model := p.opts.RerankingModels[modelID]; model != nil {
		return model, nil
	}
	if p.opts.Fallback != nil {
		return p.opts.Fallback.RerankingModel(modelID)
	}
	return nil, noSuchModel(modelID, "rerankingModel")
}

func (p *customProvider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if model := p.opts.VideoModels[modelID]; model != nil {
		return model, nil
	}
	if fallback, ok := p.opts.Fallback.(interface {
		VideoModel(string) (provider.VideoModelV3, error)
	}); ok {
		return fallback.VideoModel(modelID)
	}
	return nil, noSuchModel(modelID, "videoModel")
}

// DecisionModel implements provider.DecisionModelProvider, resolving
// decision models registered in DecisionModels before falling back to a
// Fallback provider that also implements it. Decision is experimental and
// not part of the stable Provider contract, mirroring TypeScript's
// customProvider({ decisionModels }).
func (p *customProvider) DecisionModel(modelID string) (provider.DecisionModel, error) {
	// Mirrors TypeScript: `decisionModels ?? evaluationModels`.
	if p.opts.DecisionModels != nil {
		if model := p.opts.DecisionModels[modelID]; model != nil {
			return model, nil
		}
	} else if model := p.opts.EvaluationModels[modelID]; model != nil {
		return provider.AdaptEvaluationModel(model), nil //nolint:staticcheck // deprecated compatibility path
	}

	if p.opts.Fallback != nil {
		model, ok, err := provider.DecisionModelFromProvider(p.opts.Fallback, modelID)
		if ok {
			if err != nil {
				return nil, err
			}
			if model != nil {
				return model, nil
			}
		}
	}

	return nil, noSuchModel(modelID, "decisionModel")
}

// EvaluationModel is the deprecated form of DecisionModel.
//
// Deprecated: Use DecisionModel instead.
func (p *customProvider) EvaluationModel(modelID string) (provider.EvaluationModel, error) {
	model, err := p.DecisionModel(modelID)
	if err != nil {
		return nil, err
	}
	return provider.AsEvaluationModel(model), nil
}

type customProviderWithFiles struct {
	*customProvider
}

func (p *customProviderWithFiles) Files() provider.FilesAPI {
	return resolveCustomFiles(p.customProvider)
}

type customProviderWithSkills struct {
	*customProvider
}

func (p *customProviderWithSkills) Skills() provider.SkillsAPI {
	return resolveCustomSkills(p.customProvider)
}

type customProviderWithFilesAndSkills struct {
	*customProvider
}

func (p *customProviderWithFilesAndSkills) Files() provider.FilesAPI {
	return resolveCustomFiles(p.customProvider)
}

func (p *customProviderWithFilesAndSkills) Skills() provider.SkillsAPI {
	return resolveCustomSkills(p.customProvider)
}

func resolveCustomFiles(p *customProvider) provider.FilesAPI {
	if p.opts.FilesFactory != nil {
		return p.opts.FilesFactory()
	}
	if fp, ok := p.opts.Fallback.(provider.FilesProvider); ok {
		return fp.Files()
	}
	return nil
}

func resolveCustomSkills(p *customProvider) provider.SkillsAPI {
	if p.opts.SkillsFactory != nil {
		return p.opts.SkillsFactory()
	}
	if sp, ok := p.opts.Fallback.(provider.SkillsProvider); ok {
		return sp.Skills()
	}
	return nil
}

func noSuchModel(modelID, modelType string) error {
	return providererrors.NewNoSuchModelError(modelID, modelType)
}
