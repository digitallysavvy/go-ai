package scaledown

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// utf16UnitsOfDecodedPrefix is TextDecoder.decode(prefix).length for a prefix
// of valid UTF-8: a sequence cut off at the end decodes to one U+FFFD.
func utf16UnitsOfDecodedPrefix(prefix string) int {
	units := 0
	for len(prefix) > 0 {
		r, size := utf8.DecodeRuneInString(prefix)
		if r == utf8.RuneError && size == 1 {
			// An invalid or truncated sequence: one replacement character
			// for the maximal subpart.
			units++
			if !utf8.FullRuneInString(prefix) {
				break
			}
			prefix = prefix[1:]
			continue
		}
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
		prefix = prefix[size:]
	}
	return units
}

// convertExtractionSource is TS convertScaleDownExtractionSource. ScaleDown
// reports UTF-8 byte offsets; the SDK uses UTF-16 code units. A span that does
// not line up with the supplied text yields nil, never an invented source.
func convertExtractionSource(entity scaledownExtractionEntity, state []provider.DecisionStatePart, ocrText *string) *provider.ExtractionSource {
	if entity.Start == nil || entity.End == nil || *entity.End < *entity.Start {
		return nil
	}
	entStart, entEnd := int(*entity.Start), int(*entity.End)
	if len(entity.Text) != entEnd-entStart {
		return nil
	}
	offset := 0
	filePart := -1
	for partIndex, part := range state {
		if part.Type == "file" {
			filePart = partIndex
			continue
		}
		var text string
		if part.Type == "text" {
			text = part.Text
		} else {
			encoded, err := jsonstr.Marshal(part.Value)
			if err != nil {
				return nil
			}
			text = string(encoded)
		}
		if entStart >= offset && entEnd <= offset+len(text) {
			if part.Type != "text" {
				return nil
			}
			start := utf16UnitsOfDecodedPrefix(text[:entStart-offset])
			end := utf16UnitsOfDecodedPrefix(text[:entEnd-offset])
			units := utf16.Encode([]rune(text))
			if start > len(units) {
				start = len(units)
			}
			if end > len(units) {
				end = len(units)
			}
			if end < start {
				end = start
			}
			want := utf16.Encode([]rune(entity.Text))
			slice := units[start:end]
			same := len(slice) == len(want)
			for i := 0; same && i < len(slice); i++ {
				same = slice[i] == want[i]
			}
			if same && len(string(utf16.Decode(units[:start]))) == entStart-offset {
				return &provider.ExtractionSource{Type: provider.ExtractionSourceText, Part: partIndex, Start: start, End: end}
			}
			return nil
		}
		offset += len(text) + 1 // Parts are joined with a newline.
	}
	// OCR text is appended after the supplied text. Only cite the file when
	// the span lies beyond that text and the OCR result contains the value.
	lower := offset - 1
	if lower < 0 {
		lower = 0
	}
	if filePart >= 0 && entStart >= lower && ocrText != nil && strings.Contains(*ocrText, entity.Text) {
		return &provider.ExtractionSource{Type: provider.ExtractionSourceFile, Part: filePart}
	}
	return nil
}
