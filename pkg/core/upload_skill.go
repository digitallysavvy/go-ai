package core

import (
	"context"
	"errors"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

var ErrSkillsAPINotSupported = errors.New("the provider does not support skills. Make sure it exposes a Skills() method")

type UploadSkillFile struct {
	Path string
	Data interface{}
}

type UploadSkillOptions struct {
	Files           []UploadSkillFile
	DisplayTitle    string
	ProviderOptions map[string]interface{}
}

func UploadSkill(ctx context.Context, api interface{}, opts UploadSkillOptions) (*types.UploadSkillResult, error) {
	skillsAPI, err := resolveSkillsAPI(api)
	if err != nil {
		return nil, err
	}

	files := make([]types.UploadSkillFile, 0, len(opts.Files))
	for _, f := range opts.Files {
		fd, nerr := normalizeFileData(f.Data)
		if nerr != nil {
			return nil, nerr
		}
		files = append(files, types.UploadSkillFile{
			Path: f.Path,
			Data: fd,
		})
	}

	return skillsAPI.UploadSkill(ctx, types.UploadSkillOptions{
		Files:           files,
		DisplayTitle:    opts.DisplayTitle,
		ProviderOptions: opts.ProviderOptions,
	})
}

func resolveSkillsAPI(api interface{}) (provider.SkillsAPI, error) {
	switch v := api.(type) {
	case provider.SkillsAPI:
		return v, nil
	case provider.SkillsProvider:
		skills := v.Skills()
		if skills == nil {
			return nil, ErrSkillsAPINotSupported
		}
		return skills, nil
	default:
		return nil, ErrSkillsAPINotSupported
	}
}
