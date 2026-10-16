package ai

import (
	"context"
	"errors"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Generated files a language model returns by URL are downloaded by core
// before they are exposed (TS resolveGeneratedFileData): result.Files,
// Content, the response messages and the UI file parts all carry the bytes,
// not the URL. The download uses the default SSRF-protected downloader (TS
// calls `download` directly, it does not use experimental_download) and fails
// the call with a *providererrors.DownloadError.

func generatedFileURL(file types.GeneratedFileContent) (string, bool) {
	if len(file.Data) > 0 || file.FileData.Type == types.FileDataTypeData {
		return "", false
	}
	switch {
	case file.FileData.Type == types.FileDataTypeURL && file.FileData.URL != "":
		return file.FileData.URL, true
	case file.URL != "":
		return file.URL, true
	}
	return "", false
}

func reasoningFileURL(file types.ReasoningFileContent) (string, bool) {
	if len(file.Data) > 0 || file.FileData.Type == types.FileDataTypeData {
		return "", false
	}
	if file.FileData.Type == types.FileDataTypeURL && file.FileData.URL != "" {
		return file.FileData.URL, true
	}
	return "", false
}

func downloadGeneratedFileURL(ctx context.Context, url string) ([]byte, error) {
	results, err := DefaultDownload(ctx, []DownloadRequest{{URL: url}})
	if err == nil && (len(results) != 1 || results[0] == nil) {
		err = errors.New("download returned no data")
	}
	if err != nil {
		if providererrors.IsDownloadError(err) {
			return nil, err
		}
		return nil, providererrors.NewDownloadError(url, 0, "", "", err)
	}
	return results[0].Data, nil
}

// resolveGeneratedFile returns file with its URL replaced by the downloaded
// bytes. Files that already carry data are returned unchanged.
func resolveGeneratedFile(ctx context.Context, file types.GeneratedFileContent) (types.GeneratedFileContent, error) {
	url, ok := generatedFileURL(file)
	if !ok {
		return file, nil
	}
	data, err := downloadGeneratedFileURL(ctx, url)
	if err != nil {
		return file, err
	}
	file.Data = data
	file.URL = ""
	file.FileData = types.FileData{}
	return file, nil
}

func resolveReasoningFile(ctx context.Context, file types.ReasoningFileContent) (types.ReasoningFileContent, error) {
	url, ok := reasoningFileURL(file)
	if !ok {
		return file, nil
	}
	data, err := downloadGeneratedFileURL(ctx, url)
	if err != nil {
		return file, err
	}
	file.Data = data
	file.FileData = types.FileData{}
	return file, nil
}

// resolveContentFiles downloads every URL-backed file and reasoning-file part
// of a model response, once per part. The input slice is not modified.
func resolveContentFiles(ctx context.Context, content []types.ContentPart) ([]types.ContentPart, error) {
	var out []types.ContentPart
	for i, part := range content {
		var replacement types.ContentPart
		switch p := part.(type) {
		case types.GeneratedFileContent:
			resolved, err := resolveGeneratedFile(ctx, p)
			if err != nil {
				return nil, err
			}
			replacement = resolved
		case *types.GeneratedFileContent:
			if p == nil {
				continue
			}
			resolved, err := resolveGeneratedFile(ctx, *p)
			if err != nil {
				return nil, err
			}
			replacement = resolved
		case types.ReasoningFileContent:
			resolved, err := resolveReasoningFile(ctx, p)
			if err != nil {
				return nil, err
			}
			replacement = resolved
		case *types.ReasoningFileContent:
			if p == nil {
				continue
			}
			resolved, err := resolveReasoningFile(ctx, *p)
			if err != nil {
				return nil, err
			}
			replacement = resolved
		default:
			continue
		}
		if out == nil {
			out = append([]types.ContentPart(nil), content...)
		}
		out[i] = replacement
	}
	if out == nil {
		return content, nil
	}
	return out, nil
}

// ResolveContentFiles downloads every URL-backed file and reasoning-file part
// of a model response (TS convertLanguageModelContent resolveGeneratedFileData)
// for callers outside this package, such as the legacy agent loop. The input
// slice is not modified; files that carry data are returned unchanged.
func ResolveContentFiles(ctx context.Context, content []types.ContentPart) ([]types.ContentPart, error) {
	return resolveContentFiles(ctx, content)
}
