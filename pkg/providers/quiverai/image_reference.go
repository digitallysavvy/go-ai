package quiverai

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
// mirroring the TS SDK's validateQuiverAIImageUrl: the argument is always
// "url" and the result is the WHATWG href (lower-case scheme and host, default
// port dropped, empty path "/"), like `new URL(url).toString()`.
func validateQuiverAIImageURL(rawURL, _ string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", invalidQuiverArgument("url", "QuiverAI image URLs must be valid HTTP or HTTPS URLs.")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", invalidQuiverArgument("url", "QuiverAI image URLs must use HTTP or HTTPS.")
	}
	return whatwgHref(parsed), nil
}

// whatwgHref serializes u like WHATWG URL.href for http(s) URLs.
func whatwgHref(u *url.URL) string {
	c := *u
	c.Scheme = strings.ToLower(c.Scheme)
	host := strings.ToLower(c.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := c.Port()
	if (c.Scheme == "http" && port == "80") || (c.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host += ":" + port
	}
	c.Host = host
	if c.Path == "" && c.RawPath == "" {
		c.Path = "/"
	}
	return c.String()
}

// validateQuiverAIReferenceBase64 validates a base64-encoded reference image,
// mirroring the TS SDK's validateQuiverAIReferenceBase64.
func validateQuiverAIReferenceBase64(b64, argument string) error {
	if len(b64) == 0 || len(b64) > maxReferenceBase64Length {
		return invalidQuiverArgument(argument, fmt.Sprintf("QuiverAI reference images must contain 1-%d base64 characters.", maxReferenceBase64Length))
	}
	data, err := decodeBase64LikeAtob(b64)
	if err != nil {
		return invalidQuiverArgument(argument, "QuiverAI reference image data must be valid base64.")
	}
	return validateQuiverAIReferenceBytes(data, argument)
}

// decodeBase64LikeAtob is TS convertBase64ToUint8Array: base64url characters
// are mapped to the standard alphabet and the text goes through the forgiving
// atob decode (ASCII whitespace ignored, padding optional, a length of 1 mod 4
// or any character outside the alphabet is an error).
func decodeBase64LikeAtob(s string) ([]byte, error) {
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\f', '\r':
			continue
		}
		b.WriteRune(r)
	}
	cleaned := b.String()
	if len(cleaned)%4 == 0 {
		cleaned = strings.TrimSuffix(strings.TrimSuffix(cleaned, "="), "=")
	}
	if len(cleaned)%4 == 1 {
		return nil, fmt.Errorf("invalid base64 length")
	}
	return base64.RawStdEncoding.DecodeString(cleaned)
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
var svgHeadPattern = regexp.MustCompile(`(?is)^(?:(?:<\?xml.*?\?>|<!--.*?-->|<!DOCTYPE.*?>)` + jsSpace + `*)*<svg(?:` + jsSpace + `|>)`)

// svgTailPattern and svgSelfClosingPattern mirror
// prepare-quiverai-image-reference.ts's isSvg suffix checks:
// /<\/svg>\s*$/i and /<svg(?:\s[^>]*)?\/>\s*$/is
//
// svgStrictHeadPattern is the reference-image variant (at most one xml
// declaration, comment and doctype, in that order):
// /^(?:<\?xml[\s\S]*?\?>\s*)?(?:<!--[\s\S]*?-->\s*)?(?:<!DOCTYPE[\s\S]*?>\s*)?<svg[\s>]/i
var svgStrictHeadPattern = regexp.MustCompile(`(?is)^(?:<\?xml.*?\?>` + jsSpace + `*)?(?:<!--.*?-->` + jsSpace + `*)?(?:<!DOCTYPE.*?>` + jsSpace + `*)?<svg(?:` + jsSpace + `|>)`)
var svgTailPattern = regexp.MustCompile(`(?is)</svg>` + jsSpace + `*$`)
var svgSelfClosingPattern = regexp.MustCompile(`(?is)<svg(?:` + jsSpace + `[^>]*)?/>` + jsSpace + `*$`)

// jsSpace is the JavaScript regular expression `\s` class (WhiteSpace and
// LineTerminator), wider than Go's ASCII-only `\s`.
const jsSpace = `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

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
	// TextDecoder drops a leading UTF-8 BOM; trimStart then strips JS whitespace.
	text := providerutils.JSTrimStart(strings.TrimPrefix(string(head), "\uFEFF"))
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
	text := providerutils.JSTrim(strings.TrimPrefix(string(data), "\uFEFF"))
	if !svgStrictHeadPattern.MatchString(text) {
		return false
	}
	return svgTailPattern.MatchString(text) || svgSelfClosingPattern.MatchString(text)
}

// isSvgMarkupWellFormed is a direct port of the TS isSvgMarkup validator
// (quiverai-image-model.ts): a hand-written scan, not a full XML parser, so
// it accepts and rejects exactly what the TS SDK does (any syntactically
// valid XML Name is an accepted entity reference, duplicate attributes in one
// tag are rejected, text outside the root must be whitespace, ...). Positions
// are rune indexes; JS indexes UTF-16 units, and every class used here treats
// a non-BMP character the same as the surrogate pair that encodes it.
func isSvgMarkupWellFormed(svg string) bool {
	doc := []rune(strings.TrimPrefix(svg, "\uFEFF"))
	at := func(i int) rune {
		if i < 0 || i >= len(doc) {
			return -1
		}
		return doc[i]
	}
	indexOf := func(needle string, from int) int {
		n := []rune(needle)
		for i := from; i+len(n) <= len(doc); i++ {
			if string(doc[i:i+len(n)]) == needle {
				return i
			}
		}
		return -1
	}
	startsWith := func(needle string, pos int) bool {
		n := []rune(needle)
		return pos+len(n) <= len(doc) && string(doc[pos:pos+len(n)]) == needle
	}

	var elements []string
	position := 0
	rootSeen, rootClosed, doctypeSeen := false, false, false

	for position < len(doc) {
		if doc[position] != '<' {
			end := indexOf("<", position)
			if end == -1 {
				end = len(doc)
			}
			text := string(doc[position:end])
			if (len(elements) == 0 && providerutils.JSTrim(text) != "") || strings.Contains(text, "]]>") || !hasValidXMLReferences(text) {
				return false
			}
			position = end
			continue
		}

		if startsWith("<!--", position) {
			commentEnd := indexOf("-->", position+4)
			if commentEnd == -1 || strings.Contains(string(doc[position+4:commentEnd]), "--") {
				return false
			}
			position = commentEnd + 3
			continue
		}

		if startsWith("<?", position) {
			instructionEnd := indexOf("?>", position+2)
			if instructionEnd == -1 {
				return false
			}
			position = instructionEnd + 2
			continue
		}

		if startsWith("<![CDATA[", position) {
			if len(elements) == 0 {
				return false
			}
			cdataEnd := indexOf("]]>", position+9)
			if cdataEnd == -1 {
				return false
			}
			position = cdataEnd + 3
			continue
		}

		if end := min(position+9, len(doc)); strings.ToUpper(string(doc[position:end])) == "<!DOCTYPE" {
			if c := at(position + 9); rootSeen || doctypeSeen || len(elements) > 0 || !isXMLSpace(c) {
				return false
			}
			name, nameEnd, ok := readXMLName(doc, skipXMLWhitespace(doc, position+9))
			if !ok || strings.ToLower(name) != "svg" {
				return false
			}
			doctypeEnd := findDoctypeEnd(doc, nameEnd)
			if doctypeEnd == -1 {
				return false
			}
			doctypeSeen = true
			position = doctypeEnd
			continue
		}

		if startsWith("<!", position) {
			return false
		}

		if startsWith("</", position) {
			name, nameEnd, ok := readXMLName(doc, position+2)
			if !ok {
				return false
			}
			tagEnd := skipXMLWhitespace(doc, nameEnd)
			if at(tagEnd) != '>' {
				return false
			}
			if len(elements) == 0 {
				return false
			}
			expected := elements[len(elements)-1]
			elements = elements[:len(elements)-1]
			if expected != name {
				return false
			}
			tagEnd++
			if len(elements) == 0 {
				rootClosed = true
			}
			position = tagEnd
			continue
		}

		if rootClosed {
			return false
		}

		openName, openEnd, ok := readXMLName(doc, position+1)
		if !ok {
			return false
		}
		if !rootSeen {
			if strings.ToLower(openName) != "svg" {
				return false
			}
			rootSeen = true
		}

		attributes := map[string]bool{}
		tagPosition := openEnd
		for tagPosition < len(doc) {
			beforeWhitespace := tagPosition
			tagPosition = skipXMLWhitespace(doc, tagPosition)

			if startsWith("/>", tagPosition) {
				tagPosition += 2
				if len(elements) == 0 {
					rootClosed = true
				}
				position = tagPosition
				break
			}

			if at(tagPosition) == '>' {
				elements = append(elements, openName)
				position = tagPosition + 1
				break
			}

			if tagPosition == beforeWhitespace {
				return false
			}

			attrName, attrEnd, ok := readXMLName(doc, tagPosition)
			if !ok || attributes[attrName] {
				return false
			}
			attributes[attrName] = true

			tagPosition = skipXMLWhitespace(doc, attrEnd)
			if at(tagPosition) != '=' {
				return false
			}
			tagPosition = skipXMLWhitespace(doc, tagPosition+1)

			quote := at(tagPosition)
			if quote != '"' && quote != '\'' {
				return false
			}
			valueEnd := -1
			for i := tagPosition + 1; i < len(doc); i++ {
				if doc[i] == quote {
					valueEnd = i
					break
				}
			}
			if valueEnd == -1 {
				return false
			}
			value := string(doc[tagPosition+1 : valueEnd])
			if strings.Contains(value, "<") || !hasValidXMLReferences(value) {
				return false
			}
			tagPosition = valueEnd + 1
		}

		if tagPosition >= len(doc) && position != len(doc) {
			return false
		}
	}

	return rootSeen && rootClosed && len(elements) == 0
}

func isXMLSpace(r rune) bool { return r == '\t' || r == '\n' || r == '\r' || r == ' ' }

func isXMLNameStart(r rune) bool {
	return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_' || r == ':' || r >= 0x80
}

func isXMLNameChar(r rune) bool {
	return isXMLNameStart(r) || r == '-' || r == '.' || r >= '0' && r <= '9' || r == 0xB7
}

func readXMLName(doc []rune, position int) (name string, end int, ok bool) {
	if position < 0 || position >= len(doc) || !isXMLNameStart(doc[position]) {
		return "", 0, false
	}
	start := position
	position++
	for position < len(doc) && isXMLNameChar(doc[position]) {
		position++
	}
	return string(doc[start:position]), position, true
}

func skipXMLWhitespace(doc []rune, position int) int {
	for position >= 0 && position < len(doc) && isXMLSpace(doc[position]) {
		position++
	}
	return position
}

func hasValidXMLReferences(value string) bool {
	runes := []rune(value)
	find := func(r rune, from int) int {
		for i := from; i < len(runes); i++ {
			if runes[i] == r {
				return i
			}
		}
		return -1
	}
	position := find('&', 0)
	for position != -1 {
		end := find(';', position+1)
		if end == -1 {
			return false
		}
		if !isValidXMLReference(runes[position+1 : end]) {
			return false
		}
		position = find('&', end+1)
	}
	return true
}

// isValidXMLReference matches the TS reference classes: decimal (#\d+),
// hex (#x[\dA-Fa-f]+) or a bare XML Name.
func isValidXMLReference(ref []rune) bool {
	if len(ref) == 0 {
		return false
	}
	if ref[0] == '#' {
		digits := ref[1:]
		if len(digits) > 0 && digits[0] == 'x' {
			hex := digits[1:]
			if len(hex) == 0 {
				return false
			}
			for _, r := range hex {
				if (r < '0' || r > '9') && (r < 'A' || r > 'F') && (r < 'a' || r > 'f') {
					return false
				}
			}
			return true
		}
		if len(digits) == 0 {
			return false
		}
		for _, r := range digits {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	if !isXMLNameStart(ref[0]) {
		return false
	}
	for _, r := range ref[1:] {
		if !isXMLNameChar(r) {
			return false
		}
	}
	return true
}

func findDoctypeEnd(doc []rune, position int) int {
	subsetDepth := 0
	var quote rune
	for position < len(doc) {
		c := doc[position]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[':
			subsetDepth++
		case c == ']':
			if subsetDepth == 0 {
				return -1
			}
			subsetDepth--
		case c == '>' && subsetDepth == 0:
			return position + 1
		}
		position++
	}
	return -1
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
		// TS parses the URL only to validate it and sends the original string.
		parsed, err := url.Parse(file.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, invalidQuiverArgument("files", "QuiverAI animate requires a valid HTTP or HTTPS SVG URL.")
		}
		if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
			return nil, invalidQuiverArgument("files", "QuiverAI animate requires an HTTP or HTTPS SVG URL.")
		}
		return map[string]interface{}{"url": file.URL}, nil
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
