package prompt

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

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

	// TS convertToBase64(part.data.data) returns a string payload as is, and
	// detectMediaType decodes only its sniffed prefix (decodePrefix).
	payloadString := len(file.Data) == 0 && file.FileData.Type == types.FileDataTypeData && file.FileData.DataString != ""
	payload := file.FileData.DataString
	if !payloadString {
		payload = base64.StdEncoding.EncodeToString(file.Data)
	}
	resolve := func(inline bool) (string, error) {
		if payloadString && inline && !isFullMediaType(mediaType) {
			detected, ok, err := fileutil.DetectMediaTypeSignatureBase64(payload, topLevelMediaType(mediaType))
			if err != nil {
				return "", err
			}
			if ok && detected != "" {
				return detected, nil
			}
			return "", unsupportedFilePart(fmt.Sprintf("file of media type %q must specify subtype since it could not be auto-detected", mediaType))
		}
		return resolveFullMediaTypeStrict(mediaType, file.Data, inline)
	}

	dataURL := func() (string, error) {
		full, err := resolve(inline)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("data:%s;base64,%s", full, payload), nil
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
		full, err := resolve(true)
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
			"data": payload, "format": format,
		}}, nil
	}

	if mode == FilePartsOpenAICompatible {
		switch top {
		case "application":
			if isURL {
				return nil, unsupportedFilePart("PDF file parts with URLs")
			}
			full, err := resolve(true)
			if err != nil {
				return nil, err
			}
			if full != "application/pdf" {
				return nil, unsupportedFilePart("file part media type " + full)
			}
			return pdfFilePart(file, payload), nil
		case "text":
			if isURL {
				return map[string]interface{}{"type": "text", "text": file.URL}, nil
			}
			text := file.Data
			if payloadString {
				// TS new TextDecoder().decode(convertBase64ToUint8Array(s)).
				decoded, err := fileutil.ConvertBase64ToBytes(payload)
				if err != nil {
					return nil, err
				}
				text = decoded
			}
			return map[string]interface{}{"type": "text", "text": DecodeUTF8Text(text)}, nil
		}
		return nil, unsupportedFilePart("file part media type " + mediaType)
	}

	// FilePartsOpenAIChat: everything left must be a PDF.
	full, err := resolve(inline)
	if err != nil {
		return nil, err
	}
	if full != "application/pdf" {
		return nil, unsupportedFilePart("file part media type " + full)
	}
	if isURL {
		return nil, unsupportedFilePart("PDF file parts with URLs")
	}
	return pdfFilePart(file, payload), nil
}

// pdfFilePart builds the PDF file part; payload is the base64 data (a string
// payload as given, TS convertToBase64).
func pdfFilePart(file types.FileContent, payload string) map[string]interface{} {
	fileObject := map[string]interface{}{
		"file_data": "data:application/pdf;base64," + payload,
	}
	if file.Filename != "" {
		fileObject["filename"] = file.Filename
	}
	return map[string]interface{}{"type": "file", "file": fileObject}
}

// DecodeUTF8Text is TS new TextDecoder().decode(bytes) (WHATWG UTF-8 decode,
// not fatal): a leading BOM is dropped and every maximal invalid subpart
// becomes one U+FFFD.
func DecodeUTF8Text(b []byte) string {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	out.Grow(len(b))
	for i := 0; i < len(b); {
		if r, size := utf8.DecodeRune(b[i:]); r != utf8.RuneError || size > 1 {
			out.Write(b[i : i+size])
			i += size
			continue
		}
		lead := b[i]
		need, lower, upper := 0, byte(0x80), byte(0xBF)
		switch {
		case lead >= 0xC2 && lead <= 0xDF:
			need = 1
		case lead >= 0xE0 && lead <= 0xEF:
			need = 2
			switch lead {
			case 0xE0:
				lower = 0xA0
			case 0xED:
				upper = 0x9F
			}
		case lead >= 0xF0 && lead <= 0xF4:
			need = 3
			switch lead {
			case 0xF0:
				lower = 0x90
			case 0xF4:
				upper = 0x8F
			}
		}
		i++
		for ; need > 0 && i < len(b) && b[i] >= lower && b[i] <= upper; need-- {
			i++
			lower, upper = 0x80, 0xBF
		}
		out.WriteRune(utf8.RuneError)
	}
	return out.String()
}

// ValidateToolChoiceType is the exhaustive-check default of the TS
// prepareTools tool choice switch (openai chat and Responses,
// openai-compatible, alibaba, moonshot, xai Responses): with at least one
// tool, a tool choice type other than auto, none, required or tool is an
// UnsupportedFunctionalityError "tool choice type: <type>". Without tools TS
// returns before the switch, so nothing is checked.
func ValidateToolChoiceType(tools []types.Tool, choice types.ToolChoice) error {
	if len(tools) == 0 {
		return nil
	}
	switch choice.Type {
	case "", types.ToolChoiceAuto, types.ToolChoiceNone, types.ToolChoiceRequired, types.ToolChoiceTool:
		return nil
	}
	return unsupportedFilePart("tool choice type: " + string(choice.Type))
}
