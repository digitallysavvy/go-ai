package minimax

import (
	"encoding/base64"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// minimaxImagePartOptionsSchema is minimaxImagePartOptions
// (minimax-media-options.ts): detail is the image processing detail for M3.1
// and later models. Omit it to use the server default.
var minimaxImagePartOptionsSchema = providerutils.OptionSchema{
	"detail": providerutils.OptEnum("low", "default", "high"),
}

// minimaxVideoPartOptionsSchema is minimaxVideoPartOptions: fps is the video
// frame sampling rate for M3.1 and later models, from 0.2 to 5.
var minimaxVideoPartOptionsSchema = providerutils.OptionSchema{
	"fps": providerutils.OptRange(0.2, 5),
}

// convertToMiniMaxMediaPart mirrors convertToMiniMaxMediaPart
// (convert-to-minimax-media-part.ts). It converts image and video file parts
// into MiniMax image and video blocks and returns a nil block for any other
// media type, which keeps the default Anthropic conversion.
func convertToMiniMaxMediaPart(part types.FileContent) (map[string]interface{}, error) {
	file, err := prompt.ResolveAnthropicFilePart(part)
	if err != nil {
		return nil, err
	}
	mediaKind := file.TopLevelMediaType()
	var schema providerutils.OptionSchema
	switch mediaKind {
	case "image":
		schema = minimaxImagePartOptionsSchema
	case "video":
		schema = minimaxVideoPartOptionsSchema
	default:
		return nil, nil
	}
	mediaOptions, err := providerutils.ParseProviderOptions("minimax", part.ProviderOptions, schema)
	if err != nil {
		return nil, err
	}

	var source map[string]interface{}
	switch file.Kind {
	case types.FileDataTypeData:
		mediaType, err := file.FullMediaType()
		if err != nil {
			return nil, err
		}
		source = map[string]interface{}{
			"type":       "base64",
			"media_type": mediaType,
			"data":       base64.StdEncoding.EncodeToString(file.Data),
		}
	case types.FileDataTypeURL:
		source = map[string]interface{}{"type": "url", "url": file.URL}
	case types.FileDataTypeReference:
		if mediaKind != "video" {
			return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "MiniMax image file references"}
		}
		fileID, err := providerutils.ResolveProviderReference(file.Reference, "minimax")
		if err != nil {
			return nil, err
		}
		const prefix = "mm_file://"
		if len(fileID) < len(prefix) || fileID[:len(prefix)] != prefix {
			fileID = prefix + fileID
		}
		source = map[string]interface{}{"type": "url", "url": fileID}
	default:
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("MiniMax %s input with %s data", mediaKind, file.Kind),
		}
	}

	// Only the schema's members are forwarded (zod strips unknown keys).
	for key := range schema {
		if v, ok := mediaOptions[key]; ok {
			source[key] = v
		}
	}
	return map[string]interface{}{"type": mediaKind, "source": source}, nil
}
