package core

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

var ErrFilesAPINotSupported = errors.New("the provider does not support file uploads. Make sure it exposes a Files() method")

type UploadFileOptions struct {
	Data            interface{}
	MediaType       string
	Filename        string
	ProviderOptions map[string]interface{}
}

func UploadFile(ctx context.Context, api interface{}, opts UploadFileOptions) (*types.UploadFileResult, error) {
	filesAPI, err := resolveFilesAPI(api)
	if err != nil {
		return nil, err
	}

	normalized, err := normalizeFileData(opts.Data)
	if err != nil {
		return nil, err
	}

	mediaType := opts.MediaType
	if mediaType == "" {
		if normalized.Type == types.FileDataTypeText {
			mediaType = "text/plain"
		} else if normalized.Type == types.FileDataTypeData {
			mediaType = http.DetectContentType(normalized.Data)
		}
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	return filesAPI.UploadFile(ctx, types.UploadFileOptions{
		Data:            normalized,
		MediaType:       mediaType,
		Filename:        opts.Filename,
		ProviderOptions: opts.ProviderOptions,
	})
}

func resolveFilesAPI(api interface{}) (provider.FilesAPI, error) {
	switch v := api.(type) {
	case provider.FilesAPI:
		return v, nil
	case provider.FilesProvider:
		return v.Files(), nil
	default:
		return nil, ErrFilesAPINotSupported
	}
}

func normalizeFileData(data interface{}) (types.FileData, error) {
	switch v := data.(type) {
	case []byte:
		return types.FileData{Type: types.FileDataTypeData, Data: v}, nil
	case string:
		if decoded, err := base64.StdEncoding.DecodeString(v); err == nil {
			return types.FileData{Type: types.FileDataTypeData, Data: decoded}, nil
		}
		return types.FileData{Type: types.FileDataTypeData, Data: []byte(v)}, nil
	case types.FileData:
		if v.Type == "" {
			return types.FileData{}, fmt.Errorf("file data type is required")
		}
		return v, nil
	default:
		return types.FileData{}, fmt.Errorf("unsupported file data type %T", data)
	}
}
