package registry

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type registryFilesAPI struct{}
type registrySkillsAPI struct{}

func (registryFilesAPI) UploadFile(context.Context, types.UploadFileOptions) (*types.UploadFileResult, error) {
	return &types.UploadFileResult{ProviderReference: types.ProviderReference{"custom": "file_123"}, Warnings: []types.Warning{}}, nil
}

func (registrySkillsAPI) UploadSkill(context.Context, types.UploadSkillOptions) (*types.UploadSkillResult, error) {
	return &types.UploadSkillResult{ProviderReference: types.ProviderReference{"custom": "skill_123"}, Warnings: []types.Warning{}}, nil
}

func TestCustomProviderFilesAndSkills(t *testing.T) {
	files := registryFilesAPI{}
	skills := registrySkillsAPI{}
	model := &testutil.MockLanguageModel{ProviderName: "custom", ModelName: "model-a"}

	p := NewCustomProvider(CustomProviderOptions{
		Name:           "custom",
		LanguageModels: map[string]provider.LanguageModel{"model-a": model},
		FilesFactory:   func() provider.FilesAPI { return files },
		SkillsFactory:  func() provider.SkillsAPI { return skills },
	})

	gotModel, err := p.LanguageModel("model-a")
	if err != nil {
		t.Fatalf("LanguageModel() err = %v", err)
	}
	if gotModel != model {
		t.Fatal("custom provider did not return registered model")
	}
	if p.(provider.FilesProvider).Files() == nil {
		t.Fatal("Files() = nil")
	}
	if p.(provider.SkillsProvider).Skills() == nil {
		t.Fatal("Skills() = nil")
	}
}

func TestCustomProviderOmitsUndeclaredFilesAndSkillsMethods(t *testing.T) {
	p := NewCustomProvider(CustomProviderOptions{})

	if _, ok := p.(provider.FilesProvider); ok {
		t.Fatal("custom provider without FilesFactory should not implement provider.FilesProvider")
	}
	if _, ok := p.(provider.SkillsProvider); ok {
		t.Fatal("custom provider without SkillsFactory should not implement provider.SkillsProvider")
	}
}

func TestCustomProviderInheritsFallbackFilesAndSkillsMethods(t *testing.T) {
	fallback := NewCustomProvider(CustomProviderOptions{
		FilesFactory:  func() provider.FilesAPI { return registryFilesAPI{} },
		SkillsFactory: func() provider.SkillsAPI { return registrySkillsAPI{} },
	})
	p := NewCustomProvider(CustomProviderOptions{Fallback: fallback})

	if _, ok := p.(provider.FilesProvider); !ok {
		t.Fatal("custom provider with files-capable fallback should implement provider.FilesProvider")
	}
	if _, ok := p.(provider.SkillsProvider); !ok {
		t.Fatal("custom provider with skills-capable fallback should implement provider.SkillsProvider")
	}
}

func TestRegistryResolveFilesAndSkillsAPI(t *testing.T) {
	r := NewRegistry()
	r.RegisterProvider("custom", NewCustomProvider(CustomProviderOptions{
		FilesFactory:  func() provider.FilesAPI { return registryFilesAPI{} },
		SkillsFactory: func() provider.SkillsAPI { return registrySkillsAPI{} },
	}))

	if _, err := r.ResolveFilesAPI("custom"); err != nil {
		t.Fatalf("ResolveFilesAPI() err = %v", err)
	}
	if _, err := r.ResolveSkillsAPI("custom"); err != nil {
		t.Fatalf("ResolveSkillsAPI() err = %v", err)
	}
}

func TestRegistryResolveFilesAPIUnsupported(t *testing.T) {
	r := NewRegistry()
	r.RegisterProvider("custom", NewCustomProvider(CustomProviderOptions{}))

	_, err := r.ResolveFilesAPI("custom")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `provider "custom" does not support file uploads`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
