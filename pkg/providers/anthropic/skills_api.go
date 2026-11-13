package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"io"
	"mime/multipart"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type SkillsAPI struct {
	provider *Provider
}

func (s *SkillsAPI) UploadSkill(ctx context.Context, opts types.UploadSkillOptions) (*types.UploadSkillResult, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if opts.DisplayTitle != "" {
		_ = providerutils.WriteFormDataField(writer, "display_title", opts.DisplayTitle)
	}
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

	headers := map[string]string{
		"Content-Type":   writer.FormDataContentType(),
		"anthropic-beta": BetaHeaderSkills,
	}

	resp, err := s.provider.client.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    "/skills",
		Body:    &body,
		Headers: headers,
	})
	if err != nil {
		return nil, internalhttp.UnwrapFetchRejection(err) // TS: a rejected fetch or header resolution is thrown as is
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { // TS !response.ok
		return nil, anthropicHTTPErrorText(s.provider.Name(), resp.StatusCode, resp.HTTPStatusText(), resp.Headers, resp.Body, nil)
	}
	resp.Body = []byte(textdecode.UTF8(resp.Body)) // TS response.text() before JSON.parse
	if err := anthropicSchemaValidator(s.provider.Name(), zSkillResponse)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}

	// The nullish fields are pointers: TS tests them with `!= null`, so a
	// present empty string counts.
	var out struct {
		ID            string  `json:"id"`
		DisplayTitle  string  `json:"display_title"`
		Name          *string `json:"name"`
		Description   *string `json:"description"`
		LatestVersion *string `json:"latest_version"`
		Source        string  `json:"source"`
		CreatedAt     string  `json:"created_at"`
		UpdatedAt     string  `json:"updated_at"`
	}
	if err := jsonexact.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}

	if out.LatestVersion != nil {
		// Matches the TS SDK (AnthropicSkills.uploadSkill): fetchVersionMetadata's
		// getFromApi call uses anthropicFailedResponseHandler, which throws on a
		// non-2xx response, and uploadSkill does not catch it -- the error
		// propagates to the caller rather than being silently ignored.
		// TS getFromApi reuses the beta header set only; the multipart
		// Content-Type belongs to the upload request.
		meta, err := s.fetchVersionMetadata(ctx, out.ID, *out.LatestVersion, map[string]string{"anthropic-beta": BetaHeaderSkills})
		if err != nil {
			return nil, err
		}
		// TS `versionMetadata.name ?? response.name`.
		if meta.Name != nil {
			out.Name = meta.Name
		}
		if meta.Description != nil {
			out.Description = meta.Description
		}
	}

	return &types.UploadSkillResult{
		ProviderReference: map[string]string{"anthropic": out.ID},
		DisplayTitle:      out.DisplayTitle,
		Name:              derefString(out.Name),
		Description:       derefString(out.Description),
		LatestVersion:     derefString(out.LatestVersion),
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

func (s *SkillsAPI) fetchVersionMetadata(ctx context.Context, skillID, version string, headers map[string]string) (*skillVersionMetadata, error) {
	resp, err := s.provider.client.Do(ctx, internalhttp.Request{
		Method:  "GET",
		Path:    fmt.Sprintf("/skills/%s/versions/%s", providerutils.EncodePathSegment(skillID), providerutils.EncodePathSegment(version)),
		Headers: headers,
	})
	if err != nil {
		return nil, internalhttp.UnwrapFetchRejection(err) // TS: a rejected fetch or header resolution is thrown as is
	}
	// Bug fix: this branch previously returned (nil, nil) for a non-2xx
	// response here (the combined `err != nil || !2xx`
	// check returned the transport `err`, which is nil on this path). The
	// caller's `err == nil` check then treated that as success and
	// dereferenced the nil *struct, panicking. Return a proper, non-nil API
	// error instead, matching TS's anthropicFailedResponseHandler, which
	// throws on any non-2xx response from this endpoint.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { // TS !response.ok
		return nil, anthropicHTTPErrorText(s.provider.Name(), resp.StatusCode, resp.HTTPStatusText(), resp.Headers, resp.Body, nil)
	}
	resp.Body = []byte(textdecode.UTF8(resp.Body)) // TS response.text() before JSON.parse
	if err := anthropicSchemaValidator(s.provider.Name(), zSkillVersionResponse)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}
	var out skillVersionMetadata
	if err := jsonexact.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// skillVersionMetadata is anthropicSkillVersionResponseSchema's nullish
// name and description (nil when absent or null).
type skillVersionMetadata struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// inlineFileBytes is TS convertInlineFileDataToUint8Array: stream data is
// not supported, so the stream is released unread and the call fails with
// UnsupportedFunctionalityError('streaming file upload').
func inlineFileBytes(data types.FileData) ([]byte, error) {
	switch data.Type {
	case types.FileDataTypeStream:
		if closer, ok := data.Stream.(io.Closer); ok {
			_ = closer.Close()
		}
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "streaming file upload"}
	case types.FileDataTypeData:
		if data.DataString != "" {
			return types.DecodeFileDataString(data.DataString)
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
