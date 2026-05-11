package core

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type mockSkillsAPI struct {
	last types.UploadSkillOptions
	res  *types.UploadSkillResult
	err  error
}

func (m *mockSkillsAPI) UploadSkill(_ context.Context, opts types.UploadSkillOptions) (*types.UploadSkillResult, error) {
	m.last = opts
	return m.res, m.err
}

func TestUploadSkill_NormalizesShorthand(t *testing.T) {
	api := &mockSkillsAPI{res: &types.UploadSkillResult{ProviderReference: map[string]string{"mock": "skill_1"}, Warnings: []types.Warning{}}}
	_, err := UploadSkill(context.Background(), api, UploadSkillOptions{
		Files: []UploadSkillFile{{Path: "a.txt", Data: "hello"}},
	})
	if err != nil {
		t.Fatalf("UploadSkill() error = %v", err)
	}
	if got := api.last.Files[0].Data.Type; got != types.FileDataTypeData {
		t.Fatalf("Data.Type = %q", got)
	}
	if got := api.last.Files[0].Data.DataString; got != "hello" {
		t.Fatalf("DataString = %q", got)
	}
	if len(api.last.Files[0].Data.Data) != 0 {
		t.Fatalf("Data bytes should be empty for string shorthand")
	}
}

func TestUploadSkill_MissingAPI(t *testing.T) {
	_, err := UploadSkill(context.Background(), struct{}{}, UploadSkillOptions{})
	if !errors.Is(err, ErrSkillsAPINotSupported) {
		t.Fatalf("err = %v", err)
	}
}
