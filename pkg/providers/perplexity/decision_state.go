package perplexity

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// maxDecisionImages is the number of images Perplexity Decisions accepts in
// one shared state.
const maxDecisionImages = 8

var decisionImageDataURL = regexp.MustCompile(`(?i)^data:image/(png|jpeg|webp);base64,`)

// convertToPerplexityDecisionState maps ordered decision state parts to the
// native Perplexity state: text and JSON values pass through unchanged (no
// stringifying), images become {"type":"image_url","image_url":{"url":...}}
// objects, and a single string/object/array is sent as native state while any
// other single JSON value stays inside an array because the API rejects it at
// the top level. Mirrors TS convertToPerplexityDecisionState.
func convertToPerplexityDecisionState(parts []provider.DecisionStatePart) (interface{}, error) {
	converted := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			converted = append(converted, part.Text)
		case "json":
			converted = append(converted, part.Value)
		case "file":
			image, err := perplexityDecisionImage(part)
			if err != nil {
				return nil, err
			}
			converted = append(converted, image)
		default:
			return nil, fmt.Errorf("unknown decision state part type %q", part.Type)
		}
	}

	count, err := countDecisionImages(converted)
	if err != nil {
		return nil, err
	}
	if count > maxDecisionImages {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "state",
			Message: "Perplexity Decisions supports at most 8 images in the shared state.",
		}
	}

	if len(converted) == 1 {
		switch converted[0].(type) {
		case string, map[string]interface{}, []interface{}:
			return converted[0], nil
		}
	}
	return converted, nil
}

func perplexityDecisionImage(part provider.DecisionStatePart) (map[string]interface{}, error) {
	file := part.File
	if file == nil {
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "Perplexity decision file input"}
	}
	declared := file.MediaType
	if declared == "" {
		declared = file.MimeType
	}
	if declared == "" {
		declared = file.FileData.MediaType
	}
	if kind := file.FileData.Type; kind != "" && kind != types.FileDataTypeData {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("Perplexity decision file input: %s", kind),
		}
	}

	// The base64 payload: a string is forwarded as is, bytes are encoded.
	var encoded string
	var raw []byte
	switch {
	case file.FileData.DataString != "":
		encoded = file.FileData.DataString
	case len(file.FileData.Data) > 0:
		raw = file.FileData.Data
	case len(file.Data) > 0:
		raw = file.Data
	}
	if raw != nil {
		encoded = base64.StdEncoding.EncodeToString(raw)
	}

	mediaType := declared
	if !strings.Contains(mediaType, "/") || strings.HasSuffix(mediaType, "/*") {
		// Direct DoDecide calls can bypass core's media type detection.
		detectable := raw
		if detectable == nil {
			detectable, _ = base64.StdEncoding.DecodeString(encoded)
		}
		mediaType = ""
		if detected, ok := fileutil.DetectMediaTypeSignature(detectable, "image"); ok {
			mediaType = detected
		}
	}
	switch mediaType {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("Perplexity decision image media type: %s", declared),
		}
	}
	return map[string]interface{}{
		"type":      "image_url",
		"image_url": map[string]interface{}{"url": "data:" + mediaType + ";base64," + encoded},
	}, nil
}

// countDecisionImages counts native image_url objects at any depth, rejecting
// ones that are not PNG, JPEG or WebP base64 data URLs (TS countImages).
func countDecisionImages(value interface{}) (int, error) {
	switch v := value.(type) {
	case []interface{}:
		total := 0
		for _, item := range v {
			n, err := countDecisionImages(item)
			if err != nil {
				return 0, err
			}
			total += n
		}
		return total, nil
	case map[string]interface{}:
		if v["type"] == "image_url" {
			image, _ := v["image_url"].(map[string]interface{})
			url, _ := image["url"].(string)
			if image == nil || !decisionImageDataURL.MatchString(url) {
				return 0, &providererrors.UnsupportedFunctionalityError{
					Functionality: "Perplexity decision images require PNG, JPEG, or WebP base64 data URLs",
				}
			}
			return 1, nil
		}
		total := 0
		for _, item := range v {
			n, err := countDecisionImages(item)
			if err != nil {
				return 0, err
			}
			total += n
		}
		return total, nil
	default:
		return 0, nil
	}
}
