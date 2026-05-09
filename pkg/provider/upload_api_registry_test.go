package provider

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type minimalProvider struct{}

func (minimalProvider) Name() string { return "test" }
func (minimalProvider) LanguageModel(string) (LanguageModel, error) {
	return nil, nil
}
func (minimalProvider) EmbeddingModel(string) (EmbeddingModel, error) {
	return nil, nil
}
func (minimalProvider) ImageModel(string) (ImageModel, error) {
	return nil, nil
}
func (minimalProvider) SpeechModel(string) (SpeechModel, error) {
	return nil, nil
}
func (minimalProvider) TranscriptionModel(string) (TranscriptionModel, error) {
	return nil, nil
}
func (minimalProvider) RerankingModel(string) (RerankingModel, error) {
	return nil, nil
}

type filesCapable struct{ minimalProvider }
type fakeFiles struct{}
type skillsCapable struct{ minimalProvider }
type fakeSkills struct{}

func (f filesCapable) Files() FilesAPI { return fakeFiles{} }
func (fakeFiles) UploadFile(context.Context, types.UploadFileOptions) (*types.UploadFileResult, error) {
	return &types.UploadFileResult{ProviderReference: map[string]string{"mock": "file"}, Warnings: []types.Warning{}}, nil
}

func TestResolveFilesAPI(t *testing.T) {
	if _, err := ResolveFilesAPI(filesCapable{}); err != nil {
		t.Fatalf("ResolveFilesAPI() err = %v", err)
	}
	if _, err := ResolveFilesAPI(minimalProvider{}); err == nil {
		t.Fatal("expected error for files-incapable provider")
	}
}

func (s skillsCapable) Skills() SkillsAPI { return fakeSkills{} }
func (fakeSkills) UploadSkill(context.Context, types.UploadSkillOptions) (*types.UploadSkillResult, error) {
	return &types.UploadSkillResult{ProviderReference: map[string]string{"mock": "skill"}, Warnings: []types.Warning{}}, nil
}

func TestResolveSkillsAPI(t *testing.T) {
	if _, err := ResolveSkillsAPI(skillsCapable{}); err != nil {
		t.Fatalf("ResolveSkillsAPI() err = %v", err)
	}
	if _, err := ResolveSkillsAPI(minimalProvider{}); err == nil {
		t.Fatal("expected error for skills-incapable provider")
	}
}
