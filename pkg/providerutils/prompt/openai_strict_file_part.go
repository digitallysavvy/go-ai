package prompt

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func unsupportedFilePart(functionality string) error {
	return &providererrors.UnsupportedFunctionalityError{Functionality: functionality}
}

// ResolveFullMediaType is TS resolveFullMediaType for a file part: it exposes
// the strict resolver to providers outside this package. inline reports that
// data holds the part's bytes (data.type === 'data').
func ResolveFullMediaType(mediaType string, data []byte, inline bool) (string, error) {
	return resolveFullMediaTypeStrict(mediaType, data, inline)
}

// resolveFullMediaTypeStrict is TS resolveFullMediaType for a file part: a full
// type/subtype is returned as is, a top-level-only or wildcard type is sniffed
// from inline bytes, and anything else is an UnsupportedFunctionalityError.
func resolveFullMediaTypeStrict(mediaType string, data []byte, inline bool) (string, error) {
	if isFullMediaType(mediaType) {
		return mediaType, nil
	}
	if inline {
		if detected, ok := fileutil.DetectMediaTypeSignature(data, topLevelMediaType(mediaType)); ok && detected != "" {
			return detected, nil
		}
		return "", unsupportedFilePart(fmt.Sprintf("file of media type %q must specify subtype since it could not be auto-detected", mediaType))
	}
	return "", unsupportedFilePart(fmt.Sprintf("file of media type %q must specify subtype since it is not passed as inline bytes", mediaType))
}

// openAIStrictFilePart converts a user file part the way the TS converter for
// mode does, including its UnsupportedFunctionalityError cases. It returns
// (nil, nil) when the legacy conversion should handle the part (provider
// references).
func openAIStrictFilePart(file types.FileContent, mode FilePartMode, refs *providerRefResolver) (map[string]interface{}, error) {
	if !file.FileData.IsZero() {
		if normalized, err := NormalizeFileContent(file); err == nil {
			file = normalized
		}
	}
	if file.FileData.Type == types.FileDataTypeReference || file.Reference != "" {
		return nil, nil
	}
	if file.FileData.Type == types.FileDataTypeText || file.Text != "" {
		return nil, unsupportedFilePart("text file parts")
	}

	mediaType := firstNonEmpty(file.MediaType, file.MimeType, file.FileData.MediaType)
	top := topLevelMediaType(mediaType)
	isURL := file.FileData.Type == types.FileDataTypeURL || (file.URL != "" && len(file.Data) == 0)
	inline := !isURL

	dataURL := func() (string, error) {
		full, err := resolveFullMediaTypeStrict(mediaType, file.Data, inline)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("data:%s;base64,%s", full, base64.StdEncoding.EncodeToString(file.Data)), nil
	}
	mediaURL := func() (string, error) {
		if isURL {
			return file.URL, nil
		}
		return dataURL()
	}

	if mode == FilePartsGroq {
		if top != "image" {
			return nil, unsupportedFilePart("Non-image file content parts")
		}
		url, err := mediaURL()
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": url}}, nil
	}

	if top == "image" {
		url, err := mediaURL()
		if err != nil {
			return nil, err
		}
		image := map[string]interface{}{"url": url}
		if mode == FilePartsOpenAIChat {
			if detail := openAIImageDetail(file.ProviderOptions); detail != "" {
				image["detail"] = detail
			}
		}
		return map[string]interface{}{"type": "image_url", "image_url": image}, nil
	}

	if mode == FilePartsOpenAICompatible && top == "video" {
		url, err := mediaURL()
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"type": "video_url", "video_url": map[string]interface{}{"url": url}}, nil
	}

	if top == "audio" {
		if isURL {
			return nil, unsupportedFilePart("audio file parts with URLs")
		}
		full, err := resolveFullMediaTypeStrict(mediaType, file.Data, true)
		if err != nil {
			return nil, err
		}
		var format string
		switch full {
		case "audio/wav":
			format = "wav"
		case "audio/mp3", "audio/mpeg":
			format = "mp3"
		default:
			if mode == FilePartsOpenAICompatible {
				return nil, unsupportedFilePart("audio media type " + full)
			}
			return nil, unsupportedFilePart("audio content parts with media type " + full)
		}
		return map[string]interface{}{"type": "input_audio", "input_audio": map[string]interface{}{
			"data": base64.StdEncoding.EncodeToString(file.Data), "format": format,
		}}, nil
	}

	if mode == FilePartsOpenAICompatible {
		switch top {
		case "application":
			if isURL {
				return nil, unsupportedFilePart("PDF file parts with URLs")
			}
			full, err := resolveFullMediaTypeStrict(mediaType, file.Data, true)
			if err != nil {
				return nil, err
			}
			if full != "application/pdf" {
				return nil, unsupportedFilePart("file part media type " + full)
			}
			return pdfFilePart(file), nil
		case "text":
			if isURL {
				return map[string]interface{}{"type": "text", "text": file.URL}, nil
			}
			return map[string]interface{}{"type": "text", "text": strings.ToValidUTF8(string(file.Data), "�")}, nil
		}
		return nil, unsupportedFilePart("file part media type " + mediaType)
	}

	// FilePartsOpenAIChat: everything left must be a PDF.
	full, err := resolveFullMediaTypeStrict(mediaType, file.Data, inline)
	if err != nil {
		return nil, err
	}
	if full != "application/pdf" {
		return nil, unsupportedFilePart("file part media type " + full)
	}
	if isURL {
		return nil, unsupportedFilePart("PDF file parts with URLs")
	}
	return pdfFilePart(file), nil
}

func pdfFilePart(file types.FileContent) map[string]interface{} {
	fileObject := map[string]interface{}{
		"file_data": "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(file.Data),
	}
	if file.Filename != "" {
		fileObject["filename"] = file.Filename
	}
	return map[string]interface{}{"type": "file", "file": fileObject}
}
