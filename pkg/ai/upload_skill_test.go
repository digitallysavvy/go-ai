package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type mockUploadSkillsAPI struct {
	last types.UploadSkillOptions
	res  *types.UploadSkillResult
	err  error
}

func (m *mockUploadSkillsAPI) UploadSkill(_ context.Context, opts types.UploadSkillOptions) (*types.UploadSkillResult, error) {
	m.last = opts
	return m.res, m.err
}

type mockUploadSkillsProvider struct {
	api provider.SkillsAPI
}

func (m mockUploadSkillsProvider) Skills() provider.SkillsAPI {
	return m.api
}

func TestUploadSkill_RootAPIUsesProviderSkills(t *testing.T) {
	skillsAPI := &mockUploadSkillsAPI{res: &types.UploadSkillResult{ProviderReference: map[string]string{"openai": "skill_1"}}}
	provider := mockUploadSkillsProvider{api: skillsAPI}

	_, err := UploadSkill(context.Background(), UploadSkillOptions{
		API:          provider,
		DisplayTitle: "Example Skill",
		Files: []UploadSkillFile{
			{Path: "README.md", Data: []byte("# Example")},
		},
	})
	if err != nil {
		t.Fatalf("UploadSkill() error = %v", err)
	}

	if skillsAPI.last.DisplayTitle != "Example Skill" {
		t.Fatalf("DisplayTitle = %q", skillsAPI.last.DisplayTitle)
	}
	if got := skillsAPI.last.Files[0].Path; got != "README.md" {
		t.Fatalf("Path = %q", got)
	}
	if got := string(skillsAPI.last.Files[0].Data.Data); got != "# Example" {
		t.Fatalf("Data = %q", got)
	}
}

func TestUploadSkill_StringShorthandMatchesTypeScriptDataString(t *testing.T) {
	api := &mockUploadSkillsAPI{res: &types.UploadSkillResult{ProviderReference: map[string]string{"openai": "skill_1"}}}

	_, err := UploadSkill(context.Background(), UploadSkillOptions{
		API: api,
		Files: []UploadSkillFile{
			{Path: "asset.txt", Data: "aGVsbG8="},
		},
	})
	if err != nil {
		t.Fatalf("UploadSkill() error = %v", err)
	}

	data := skillsAPIFileData(t, api.last.Files, 0)
	if data.Type != types.FileDataTypeData {
		t.Fatalf("Data.Type = %q", data.Type)
	}
	if data.DataString != "aGVsbG8=" {
		t.Fatalf("DataString = %q", data.DataString)
	}
	if len(data.Data) != 0 {
		t.Fatalf("Data bytes should be empty for string shorthand")
	}
}

func TestUploadSkill_UnsupportedAPI(t *testing.T) {
	_, err := UploadSkill(context.Background(), UploadSkillOptions{API: struct{}{}})
	if !errors.Is(err, ErrSkillsAPINotSupported) {
		t.Fatalf("err = %v", err)
	}
}

func skillsAPIFileData(t *testing.T, files []types.UploadSkillFile, index int) types.FileData {
	t.Helper()
	if len(files) <= index {
		t.Fatalf("missing file at index %d", index)
	}
	return files[index].Data
}
