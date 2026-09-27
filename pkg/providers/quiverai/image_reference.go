package quiverai

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const (
	maxReferenceBase64Length = 16_777_216
	maxReferenceBytes        = 12_582_912
	maxAnimationSourceB64Len = 1_066_668
	maxEditSVGBytes          = 200_000
)

var supportedReferenceMediaTypes = map[string]bool{
	"image/gif":     true,
	"image/jpeg":    true,
	"image/png":     true,
	"image/svg+xml": true,
	"image/webp":    true,
}

// validateQuiverAIImageURL validates that url is a parseable HTTP/HTTPS URL,
// mirroring the TS SDK's validateQuiverAIImageUrl.
func validateQuiverAIImageURL(rawURL, argument string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", invalidQuiverArgument(argument, "QuiverAI image URLs must be valid HTTP or HTTPS URLs.")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", invalidQuiverArgument(argument, "QuiverAI image URLs must use HTTP or HTTPS.")
	}
	return parsed.String(), nil
}

// validateQuiverAIReferenceBase64 validates a base64-encoded reference image,
// mirroring the TS SDK's validateQuiverAIReferenceBase64.
func validateQuiverAIReferenceBase64(b64, argument string) error {
	if len(b64) == 0 || len(b64) > maxReferenceBase64Length {
		return invalidQuiverArgument(argument, fmt.Sprintf("QuiverAI reference images must contain 1-%d base64 characters.", maxReferenceBase64Length))
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return invalidQuiverArgument(argument, "QuiverAI reference image data must be valid base64.")
	}
	return validateQuiverAIReferenceBytes(data, argument)
}

// validateQuiverAIReferenceBytes validates decoded reference image bytes,
// mirroring the TS SDK's validateQuiverAIReferenceBytes.
func validateQuiverAIReferenceBytes(data []byte, argument string) error {
	if len(data) == 0 || len(data) > maxReferenceBytes {
		return invalidQuiverArgument(argument, fmt.Sprintf("QuiverAI reference images must decode to 1-%d bytes.", maxReferenceBytes))
	}
	var mediaType string
	if isSVGBytes(data) {
		mediaType = "image/svg+xml"
	} else if detected, ok := fileutil.DetectMediaTypeSignature(data, "image"); ok {
		mediaType = detected
	}
	if mediaType == "" || !supportedReferenceMediaTypes[mediaType] {
		return invalidQuiverArgument(argument, "QuiverAI reference images must be PNG, JPEG, WebP, GIF, or SVG data.")
	}
	return nil
}

// svgHeadPattern mirrors the TS SDK's isSvg head regex:
// /^(?:(?:<\?xml[\s\S]*?\?>|<!--[\s\S]*?-->|<!DOCTYPE[\s\S]*?>)\s*)*<svg(?:\s|>)/i
var svgHeadPattern = regexp.MustCompile(`(?is)^(?:(?:<\?xml.*?\?>|<!--.*?-->|<!DOCTYPE.*?>)\s*)*<svg(?:\s|>)`)

// svgTailPattern and svgSelfClosingPattern mirror
// prepare-quiverai-image-reference.ts's isSvg suffix checks:
// /<\/svg>\s*$/i and /<svg(?:\s[^>]*)?\/>\s*$/is
var svgTailPattern = regexp.MustCompile(`(?is)</svg>\s*$`)
var svgSelfClosingPattern = regexp.MustCompile(`(?is)<svg(?:\s[^>]*)?/>\s*$`)

const maxSVGHeadSniffBytes = 4096

// isSVGHead reports whether data begins with an SVG root element, mirroring
// the TS SDK's quiverai-image-model.ts isSvg helper (a lenient, head-only
// check used for the `animate` source; it tolerates trailing bytes and
// invalid UTF-8, matching TextDecoder's { fatal: false } decode).
func isSVGHead(data []byte) bool {
	head := data
	if len(head) > maxSVGHeadSniffBytes {
		head = head[:maxSVGHeadSniffBytes]
	}
	text := strings.TrimLeft(string(head), " \t\n\r\f\v")
	return svgHeadPattern.MatchString(text)
}

// isSVGBytes reports whether data is a complete, well-formed-looking SVG
// document (head AND tail match), mirroring
// prepare-quiverai-image-reference.ts's stricter isSvg helper used for
// reference-image media type detection. Unlike isSVGHead, this requires valid
// UTF-8 (TextDecoder's { fatal: true } decode).
func isSVGBytes(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	text := strings.TrimSpace(strings.TrimPrefix(string(data), string(rune(0xFEFF))))
	if !svgHeadPattern.MatchString(text) {
		return false
	}
	return svgTailPattern.MatchString(text) || svgSelfClosingPattern.MatchString(text)
}

// isSvgMarkupWellFormed reports whether svg is a single well-formed XML
// document whose root element is <svg>. This uses Go's encoding/xml decoder
// for structural well-formedness (unbalanced/mismatched tags, multiple roots)
// rather than porting the TS SDK's hand-written XML validator byte-for-byte;
// it rejects the same class of malformed input (e.g. "<svg><g></svg>").
//
// dec.Entity is set to xml.HTMLEntity so named HTML entity references (e.g.
// "&nbsp;", "&copy;") don't fail decoding as unknown entities. TS's own
// validator (hasValidXmlReferences in quiverai-image-model.ts) is even more
// permissive -- it accepts ANY syntactically valid XML Name as a named
// reference, not just the standard HTML set, plus numeric character
// references (which Go's decoder already resolves without an Entity map).
func isSvgMarkupWellFormed(svg string) bool {
	dec := xml.NewDecoder(strings.NewReader(svg))
	dec.Strict = true
	dec.Entity = xml.HTMLEntity

	var rootName string
	depth := 0
	sawRoot := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				if sawRoot {
					// A second top-level element after the root closed.
					return false
				}
				rootName = strings.ToLower(t.Name.Local)
				if rootName != "svg" {
					return false
				}
			}
			depth++
		case xml.EndElement:
			depth--
			if depth == 0 {
				sawRoot = true
			}
			if depth < 0 {
				return false
			}
		case xml.CharData:
			if depth == 0 && len(strings.TrimSpace(string(t))) > 0 {
				return false
			}
		}
	}
	return sawRoot && depth == 0 && rootName == "svg"
}

// toQuiverAIImageReference converts an ImageFile into the JSON-safe reference
// shape accepted by QuiverAI (used for `generate` references and the
// `vectorize` input image). No validation is applied, mirroring the TS SDK's
// toQuiverAIImageReference.
func toQuiverAIImageReference(file provider.ImageFile) map[string]interface{} {
	if file.Type == "url" || file.URL != "" {
		return map[string]interface{}{"url": file.URL}
	}
	return map[string]interface{}{"base64": base64.StdEncoding.EncodeToString(file.Data)}
}

// toQuiverAIEditSource converts the SVG editing source file into the
// svg_source wire shape, validating that it is a well-formed, size-bounded
// UTF-8 SVG document (for inline data) or a valid HTTP(S) URL.
func toQuiverAIEditSource(file provider.ImageFile) (map[string]interface{}, error) {
	if file.Type == "url" || (file.URL != "" && len(file.Data) == 0) {
		validated, err := validateQuiverAIImageURL(file.URL, "files")
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"svg_source": map[string]interface{}{"url": validated}}, nil
	}

	data := file.Data
	if len(data) == 0 || len(data) > maxEditSVGBytes {
		return nil, invalidQuiverArgument("files", fmt.Sprintf("QuiverAI SVG source data must contain 1-%d bytes.", maxEditSVGBytes))
	}
	if !utf8.Valid(data) {
		return nil, invalidQuiverArgument("files", "QuiverAI SVG source data must be valid UTF-8.")
	}
	svg := string(data)
	if len(svg) > maxEditSVGBytes || !isSvgMarkupWellFormed(svg) {
		return nil, invalidQuiverArgument("files", "QuiverAI SVG source data must contain a complete SVG document.")
	}
	return map[string]interface{}{"svg_source": map[string]interface{}{"base64": base64.StdEncoding.EncodeToString(data)}}, nil
}

// toQuiverAIAnimationSource converts the animation source file into the
// svg_source wire shape, validating it is SVG data within the API's size
// limit (for inline data) or a valid HTTP(S) URL.
func toQuiverAIAnimationSource(file provider.ImageFile) (map[string]interface{}, error) {
	if file.Type == "url" || (file.URL != "" && len(file.Data) == 0) {
		validated, err := validateQuiverAIImageURL(file.URL, "files")
		if err != nil {
			return nil, invalidQuiverArgument("files", "QuiverAI animate requires a valid HTTP or HTTPS SVG URL.")
		}
		return map[string]interface{}{"url": validated}, nil
	}

	data := file.Data
	if !isSVGHead(data) {
		return nil, invalidQuiverArgument("files", "QuiverAI animate requires the input file to contain SVG data.")
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	if len(b64) > maxAnimationSourceB64Len {
		return nil, invalidQuiverArgument("files", fmt.Sprintf("QuiverAI animate accepts at most %d base64 characters for the source SVG.", maxAnimationSourceB64Len))
	}
	return map[string]interface{}{"base64": b64}, nil
}

// referenceImagesToWire converts validated providerOptions.quiverai.referenceImages
// entries into the wire shape expected by the SVG editing endpoint.
func referenceImagesToWire(refs []QuiverAIReferenceImageOption) ([]map[string]interface{}, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	result := make([]map[string]interface{}, 0, len(refs))
	for i, ref := range refs {
		argument := fmt.Sprintf("providerOptions.quiverai.referenceImages[%d]", i)
		if ref.URL != "" {
			validated, err := validateQuiverAIImageURL(ref.URL, argument)
			if err != nil {
				return nil, err
			}
			result = append(result, map[string]interface{}{"url": validated})
			continue
		}
		if err := validateQuiverAIReferenceBase64(ref.Base64, argument); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{"base64": ref.Base64})
	}
	return result, nil
}
