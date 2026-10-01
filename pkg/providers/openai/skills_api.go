package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type SkillsAPI struct {
	provider *Provider
}

func (s *SkillsAPI) UploadSkill(ctx context.Context, opts types.UploadSkillOptions) (*types.UploadSkillResult, error) {
	var warnings []types.Warning
	if opts.DisplayTitle != "" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "displayTitle"})
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range opts.Files {
		part, err := writer.CreateFormFile("files[]", chooseFilename(file.Path))
		if err != nil {
			return nil, err
		}
		content, err := inlineFileBytes(file.Data)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(content); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	resp, err := s.provider.client.Do(ctx, internalhttp.Request{
		Method: "POST",
		Path:   "/skills",
		Body:   &body,
		Headers: map[string]string{
			"Content-Type": writer.FormDataContentType(),
		},
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai skills upload failed: %d %s", resp.StatusCode, string(resp.Body))
	}

	var out struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Description    string `json:"description"`
		DefaultVersion string `json:"default_version"`
		LatestVersion  string `json:"latest_version"`
		CreatedAt      *int64 `json:"created_at"`
		UpdatedAt      *int64 `json:"updated_at"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}

	meta := map[string]interface{}{}
	if out.DefaultVersion != "" {
		meta["defaultVersion"] = out.DefaultVersion
	}
	if out.CreatedAt != nil {
		meta["createdAt"] = *out.CreatedAt
	}
	if out.UpdatedAt != nil {
		meta["updatedAt"] = *out.UpdatedAt
	}

	return &types.UploadSkillResult{
		ProviderReference: map[string]string{"openai": out.ID},
		Name:              out.Name,
		Description:       out.Description,
		LatestVersion:     out.LatestVersion,
		ProviderMetadata:  map[string]interface{}{"openai": meta},
		Warnings:          warnings,
	}, nil
}
