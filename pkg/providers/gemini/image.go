package gemini

import (
	"encoding/base64"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageCallOptions builds the language-model provider options an image model
// passes to its underlying Gemini language model. It ports the TS
// google-image-model.ts / google-vertex-image-model.ts option handling: the
// caller's options pass through unchanged except that responseModalities is
// forced to ["IMAGE"] (a caller-supplied value is dropped), googleSearch is
// returned separately (it becomes a tool), and imageConfig is the caller's
// imageConfig overlaid with the call's aspectRatio.
//
// legacyImageSizeKeys name top-level Go-only options (e.g. "imageSize",
// "sampleImageSize") that predate imageConfig; they fill imageConfig.imageSize
// when imageConfig does not set it.
func ImageCallOptions(user map[string]interface{}, aspectRatio string, legacyImageSizeKeys ...string) (opts map[string]interface{}, googleSearch interface{}) {
	opts = map[string]interface{}{}
	for k, v := range user {
		switch k {
		case "googleSearch":
			googleSearch = v
		case "responseModalities", "imageConfig":
			// forced / merged below
		default:
			if v != nil {
				opts[k] = v
			}
		}
	}
	opts["responseModalities"] = []string{"IMAGE"}

	imageConfig := map[string]interface{}{}
	if ic, ok := user["imageConfig"].(map[string]interface{}); ok {
		for k, v := range ic {
			imageConfig[k] = v
		}
	}
	if aspectRatio != "" {
		imageConfig["aspectRatio"] = aspectRatio
	}
	for _, key := range legacyImageSizeKeys {
		delete(opts, key)
		if v, ok := user[key]; ok && v != nil {
			if _, set := imageConfig["imageSize"]; !set {
				imageConfig["imageSize"] = v
			}
		}
	}
	if len(imageConfig) > 0 {
		opts["imageConfig"] = imageConfig
	}
	return opts, googleSearch
}

// ImageOutput is the image data extracted from a language-model result.
type ImageOutput struct {
	Images       [][]byte
	Base64Images []string
	MediaType    string // media type of the first image
}

// ExtractImages collects every inline image file part of a generate result,
// like TS (`part.type === 'file' && mediaType.startsWith('image/')`).
func ExtractImages(content []types.ContentPart) ImageOutput {
	var out ImageOutput
	for _, part := range content {
		file, ok := part.(types.GeneratedFileContent)
		if !ok || !strings.HasPrefix(file.MediaType, "image/") || len(file.Data) == 0 {
			continue
		}
		if out.MediaType == "" {
			out.MediaType = file.MediaType
		}
		out.Images = append(out.Images, file.Data)
		out.Base64Images = append(out.Base64Images, base64.StdEncoding.EncodeToString(file.Data))
	}
	return out
}
