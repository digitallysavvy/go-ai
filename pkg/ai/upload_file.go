package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

var ErrFilesAPINotSupported = errors.New("the provider does not support file uploads. Make sure it exposes a Files() method")

type UploadFileOptions struct {
	API             interface{}
	Data            interface{}
	MediaType       string
	Filename        string
	ProviderOptions map[string]interface{}
}

func UploadFile(ctx context.Context, opts UploadFileOptions) (*types.UploadFileResult, error) {
	filesAPI, err := resolveFilesAPI(opts.API)
	if err != nil {
		return nil, err
	}

	normalized, err := normalizeUploadFileData(opts.Data)
	if err != nil {
		return nil, err
	}

	mediaType := opts.MediaType
	if mediaType == "" {
		if normalized.Type == types.FileDataTypeText {
			mediaType = "text/plain"
		} else if normalized.Type == types.FileDataTypeData {
			dataBytes, derr := uploadFileDataBytes(normalized)
			if derr != nil {
				return nil, derr
			}
			mediaType = detectUploadMediaType(dataBytes)
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
		files := v.Files()
		if files == nil {
			return nil, ErrFilesAPINotSupported
		}
		return files, nil
	default:
		return nil, ErrFilesAPINotSupported
	}
}

func uploadFileDataBytes(data types.FileData) ([]byte, error) {
	if len(data.Data) > 0 || data.DataString == "" {
		return data.Data, nil
	}
	decoded, err := types.DecodeFileDataString(data.DataString)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 file data")
	}
	return decoded, nil
}

func normalizeUploadFileData(data interface{}) (types.FileData, error) {
	switch v := data.(type) {
	case []byte:
		return types.FileData{Type: types.FileDataTypeData, Data: v}, nil
	case string:
		return types.FileData{Type: types.FileDataTypeData, DataString: v}, nil
	case types.FileData:
		if v.Type == "" {
			return types.FileData{}, fmt.Errorf("file data type is required")
		}
		return v, nil
	default:
		return types.FileData{}, fmt.Errorf("unsupported file data type %T", data)
	}
}

func detectUploadMediaType(data []byte) string {
	detected := http.DetectContentType(data)
	if strings.HasPrefix(detected, "text/plain") {
		return "text/plain"
	}
	if detected != "" && detected != "application/octet-stream" {
		return detected
	}
	if isLikelyUploadText(data) {
		return "text/plain"
	}
	return "application/octet-stream"
}

func isLikelyUploadText(data []byte) bool {
	const checkLength = 512
	if len(data) == 0 {
		return false
	}
	limit := len(data)
	if limit > checkLength {
		limit = checkLength
	}
	for i := 0; i < limit; i++ {
		b := data[i]
		if b == 0x00 || (b < 0x20 && b != 0x09 && b != 0x0a && b != 0x0d) {
			return false
		}
	}
	return true
}
