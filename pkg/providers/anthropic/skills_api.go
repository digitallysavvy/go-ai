package anthropic

import (
	"bytes"
	"context"
	"encoding/base64"
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
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if opts.DisplayTitle != "" {
		_ = writer.WriteField("display_title", opts.DisplayTitle)
	}
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

	headers := map[string]string{
		"Content-Type":   writer.FormDataContentType(),
		"anthropic-beta": BetaHeaderSkills,
	}

	resp, err := s.provider.client.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    "/v1/skills",
		Body:    &body,
		Headers: headers,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("anthropic skills upload failed: %d %s", resp.StatusCode, string(resp.Body))
	}

	var out struct {
		ID            string `json:"id"`
		DisplayTitle  string `json:"display_title"`
		Name          string `json:"name"`
		Description   string `json:"description"`
		LatestVersion string `json:"latest_version"`
		Source        string `json:"source"`
		CreatedAt     string `json:"created_at"`
		UpdatedAt     string `json:"updated_at"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}

	if out.LatestVersion != "" {
		meta, err := s.fetchVersionMetadata(ctx, out.ID, out.LatestVersion, headers)
		if err == nil {
			if meta.Name != "" {
				out.Name = meta.Name
			}
			if meta.Description != "" {
				out.Description = meta.Description
			}
		}
	}

	return &types.UploadSkillResult{
		ProviderReference: map[string]string{"anthropic": out.ID},
		DisplayTitle:      out.DisplayTitle,
		Name:              out.Name,
		Description:       out.Description,
		LatestVersion:     out.LatestVersion,
		ProviderMetadata: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"source":    out.Source,
				"createdAt": out.CreatedAt,
				"updatedAt": out.UpdatedAt,
			},
		},
		Warnings: []types.Warning{},
	}, nil
}

func (s *SkillsAPI) fetchVersionMetadata(ctx context.Context, skillID, version string, headers map[string]string) (*struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}, error) {
	resp, err := s.provider.client.Do(ctx, internalhttp.Request{
		Method:  "GET",
		Path:    fmt.Sprintf("/v1/skills/%s/versions/%s", skillID, version),
		Headers: headers,
	})
	if err != nil || resp.StatusCode >= 400 {
		return nil, err
	}
	var out struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func inlineFileBytes(data types.FileData) ([]byte, error) {
	switch data.Type {
	case types.FileDataTypeData:
		if data.DataString != "" {
			return base64.StdEncoding.DecodeString(data.DataString)
		}
		return data.Data, nil
	case types.FileDataTypeText:
		return []byte(data.Text), nil
	default:
		return nil, fmt.Errorf("unsupported file data type %q", data.Type)
	}
}

func chooseFilename(filename string) string {
	if filename == "" {
		return "blob"
	}
	return filename
}
