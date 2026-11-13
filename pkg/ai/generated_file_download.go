package ai

import (
	"context"
	"errors"

	"github.com/digitallysavvy/go-ai/pkg/internal/urlnorm"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Generated files a language model returns by URL are downloaded by core
// before they are exposed (TS resolveGeneratedFileData): result.Files,
// Content, the response messages and the UI file parts all carry the bytes,
// not the URL. The download uses the call's ExperimentalDownload when one is
// given (TS 2ebc78b654: experimental_download also handles generated file
// URLs, called with isUrlSupportedByModel false, and a nil result is a
// DownloadError), else the default validated, DNS-pinned downloader, which
// fails the call with a *providererrors.DownloadError.

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

// downloadGeneratedFileURL downloads a generated file URL. The URL is the
// WHATWG serialization TS hands the download (the provider's `new URL(...)`,
// url.toString()); a URL that does not parse is used as given.
func downloadGeneratedFileURL(ctx context.Context, url string, download DownloadFunction) ([]byte, error) {
	if href, err := urlnorm.Href(url); err == nil {
		url = href
	}
	if download != nil {
		return downloadGeneratedFileWith(ctx, url, download)
	}
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

// downloadGeneratedFileWith downloads a generated file URL (already
// serialized) with the call's download function (TS downloadGeneratedFile):
// generated files have no model to pass the URL through to, so a nil result
// cannot be honored. The function's own error is returned as is.
func downloadGeneratedFileWith(ctx context.Context, url string, download DownloadFunction) ([]byte, error) {
	results, err := download(ctx, []DownloadRequest{{URL: url, IsURLSupportedByModel: false}})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 || results[0] == nil {
		return nil, providererrors.NewDownloadError(url, 0, "", "Generated file URL "+url+" must be downloaded, but the download function returned null", nil)
	}
	return results[0].Data, nil
}

// resolveGeneratedFileWith returns file with its URL replaced by the bytes
// the call's ExperimentalDownload returns (nil: the default downloader).
// Files that already carry data are returned unchanged.
func resolveGeneratedFileWith(ctx context.Context, file types.GeneratedFileContent, download DownloadFunction) (types.GeneratedFileContent, error) {
	url, ok := generatedFileURL(file)
	if !ok {
		return file, nil
	}
	data, err := downloadGeneratedFileURL(ctx, url, download)
	if err != nil {
		return file, err
	}
	file.Data = data
	file.URL = ""
	file.FileData = types.FileData{}
	return file, nil
}

// resolveReasoningFileWith is resolveGeneratedFileWith for a reasoning file.
func resolveReasoningFileWith(ctx context.Context, file types.ReasoningFileContent, download DownloadFunction) (types.ReasoningFileContent, error) {
	url, ok := reasoningFileURL(file)
	if !ok {
		return file, nil
	}
	data, err := downloadGeneratedFileURL(ctx, url, download)
	if err != nil {
		return file, err
	}
	file.Data = data
	file.FileData = types.FileData{}
	return file, nil
}

// resolveContentFiles downloads every URL-backed file and reasoning-file part
// of a model response with the default downloader.
func resolveContentFiles(ctx context.Context, content []types.ContentPart) ([]types.ContentPart, error) {
	return resolveContentFilesWith(ctx, content, nil)
}

// resolveContentFilesWith downloads every URL-backed file and reasoning-file
// part of a model response, once per part, with the call's download
// function (nil: the default downloader). The input slice is not modified.
func resolveContentFilesWith(ctx context.Context, content []types.ContentPart, download DownloadFunction) ([]types.ContentPart, error) {
	var out []types.ContentPart
	for i, part := range content {
		var replacement types.ContentPart
		switch p := part.(type) {
		case types.GeneratedFileContent:
			resolved, err := resolveGeneratedFileWith(ctx, p, download)
			if err != nil {
				return nil, err
			}
			replacement = resolved
		case *types.GeneratedFileContent:
			if p == nil {
				continue
			}
			resolved, err := resolveGeneratedFileWith(ctx, *p, download)
			if err != nil {
				return nil, err
			}
			replacement = resolved
		case types.ReasoningFileContent:
			resolved, err := resolveReasoningFileWith(ctx, p, download)
			if err != nil {
				return nil, err
			}
			replacement = resolved
		case *types.ReasoningFileContent:
			if p == nil {
				continue
			}
			resolved, err := resolveReasoningFileWith(ctx, *p, download)
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
