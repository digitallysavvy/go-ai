package openai

import (
	"bytes"
	"context"
	"mime/multipart"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

type SkillsAPI struct {
	provider *Provider
}

func (s *SkillsAPI) UploadSkill(ctx context.Context, opts types.UploadSkillOptions) (*types.UploadSkillResult, error) {
	warnings := []types.Warning{}
	if opts.DisplayTitle != "" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "displayTitle"})
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range opts.Files {
		// TS appends new Blob([content]) (no type) named file.path; a File
		// named "" has no filename parameter.
		part, err := providerutils.CreateFormDataFile(writer, "files[]", file.Path, "")
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
		return nil, internalhttp.UnwrapFetchRejection(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { // TS !response.ok
		return nil, openAICompatStatusError("openai", resp.StatusCode, resp.Headers, resp.Body)
	}

	resp.Body = []byte(textdecode.UTF8(resp.Body)) // TS response.text(): BOM dropped, invalid UTF-8 replaced
	if err := providerutils.MediaResponseValidator("openai", providerutils.SchemaOpenAISkill)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}
	var out struct {
		ID             string   `json:"id"`
		Name           string   `json:"name"`
		Description    string   `json:"description"`
		DefaultVersion *string  `json:"default_version"`
		LatestVersion  string   `json:"latest_version"`
		CreatedAt      *float64 `json:"created_at"`
		UpdatedAt      *float64 `json:"updated_at"`
	}
	if err := jsonexact.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}

	meta := map[string]interface{}{}
	// TS `default_version != null`: a present empty string is kept.
	if out.DefaultVersion != nil {
		meta["defaultVersion"] = *out.DefaultVersion
	}
	if out.CreatedAt != nil {
		meta["createdAt"] = unixSecondsMetadataValue(*out.CreatedAt)
	}
	if out.UpdatedAt != nil {
		meta["updatedAt"] = unixSecondsMetadataValue(*out.UpdatedAt)
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
