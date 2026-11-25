package scaledown

import (
	"encoding/base64"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// documentMediaTypes are the document media types ScaleDown accepts.
var documentMediaTypes = []string{"image/jpeg", "image/png", "image/tiff", "application/pdf"}

// convertToScaleDownState is TS convertToScaleDownState: text and JSON parts
// are joined with newlines into "text", and at most one JPEG, PNG, TIFF or PDF
// file becomes "document" with its "document_mime_type". Keys are in the order
// text, document, document_mime_type, each only when present.
func convertToScaleDownState(parts []provider.DecisionStatePart) (providerutils.OrderedMap, error) {
	var text []string
	var document, documentMimeType string
	haveDocument := false
	for _, part := range parts {
		if part.Type != "file" {
			if part.Type == "text" {
				text = append(text, part.Text)
				continue
			}
			encoded, err := jsonstr.Marshal(part.Value)
			if err != nil {
				return providerutils.OrderedMap{}, err
			}
			text = append(text, string(encoded))
			continue
		}
		file := part.File
		unsupported := &providererrors.UnsupportedFunctionalityError{Functionality: "ScaleDown supports one JPEG, PNG, TIFF, or PDF document."}
		if haveDocument || file == nil {
			return providerutils.OrderedMap{}, unsupported
		}
		mediaType := file.MediaType
		if mediaType == "" {
			mediaType = file.MimeType
		}
		if mediaType == "" {
			mediaType = file.FileData.MediaType
		}
		if dataType := file.FileData.Type; dataType != "" && dataType != types.FileDataTypeData {
			return providerutils.OrderedMap{}, unsupported
		}
		allowed := false
		for _, candidate := range documentMediaTypes {
			if candidate == mediaType {
				allowed = true
			}
		}
		if !allowed {
			return providerutils.OrderedMap{}, unsupported
		}
		// A base64 string is sent unchanged; bytes are encoded.
		document = file.FileData.DataString
		if document == "" {
			raw := file.FileData.Data
			if len(raw) == 0 {
				raw = file.Data
			}
			document = base64.StdEncoding.EncodeToString(raw)
		}
		documentMimeType, haveDocument = mediaType, true
	}

	state := providerutils.OrderedMap{Vals: map[string]interface{}{}}
	if len(text) > 0 {
		state.Keys = append(state.Keys, "text")
		state.Vals["text"] = strings.Join(text, "\n")
	}
	if haveDocument {
		state.Keys = append(state.Keys, "document", "document_mime_type")
		state.Vals["document"] = document
		state.Vals["document_mime_type"] = documentMimeType
	}
	return state, nil
}
