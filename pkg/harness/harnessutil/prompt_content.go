package harnessutil

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// PromptContentPart is one normalized part of a harness prompt: either a text
// part or an inline base64 image. Mirrors TS `HarnessPromptContentPart`.
type PromptContentPart struct {
	// Type is "text" or "image".
	Type string `json:"type"`
	// Text is set for text parts.
	Text string `json:"text,omitempty"`
	// Data is the base64 payload of an image part.
	Data string `json:"data,omitempty"`
	// MediaType is the full media type of an image part (e.g. "image/png").
	MediaType string `json:"mediaType,omitempty"`
}

// PromptContent is the normalized user prompt handed to bridge adapters:
// either plain text or an ordered list of text and inline image parts. Mirrors
// TS `HarnessPromptContent` (`string | HarnessPromptContentPart[]`).
//
// When Parts is nil the prompt is the plain string Text.
type PromptContent struct {
	Text  string
	Parts []PromptContentPart
}

// IsText reports whether the prompt is plain text.
func (c PromptContent) IsText() bool { return c.Parts == nil }

// MarshalJSON encodes the prompt as a JSON string or an array of parts, the
// shape of TS `harnessPromptContentSchema`.
func (c PromptContent) MarshalJSON() ([]byte, error) {
	if c.Parts != nil {
		return json.Marshal(c.Parts)
	}
	return json.Marshal(c.Text)
}

// UnmarshalJSON accepts a JSON string or an array of text/image parts and
// rejects any other part type, like TS `harnessPromptContentSchema`.
func (c *PromptContent) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*c = PromptContent{Text: text}
		return nil
	}
	var parts []PromptContentPart
	if err := json.Unmarshal(data, &parts); err != nil {
		return fmt.Errorf("harness prompt content must be a string or an array of text and image parts: %w", err)
	}
	if parts == nil {
		parts = []PromptContentPart{}
	}
	for _, p := range parts {
		switch p.Type {
		case "text":
		case "image":
			if p.MediaType == "" {
				return fmt.Errorf("harness prompt image part requires mediaType")
			}
		default:
			return fmt.Errorf("unsupported harness prompt content part type %q", p.Type)
		}
	}
	*c = PromptContent{Parts: parts}
	return nil
}

// ExtractPromptContent normalizes a harness prompt into plain text, or into
// ordered text and inline image parts. Mirrors TS
// `extractHarnessPromptContent()`.
//
// A bare string or a text-only message collapses to a string (text parts
// joined with a blank line). Images may be passed as bytes, base64 strings or
// base64 data: URLs. Remote image URLs, provider references, non-image files
// and unknown content-part types are rejected with a
// *harness.CapabilityUnsupportedError (no harness ID, like TS).
func ExtractPromptContent(prompt harness.Prompt) (PromptContent, error) {
	if prompt.Message == nil {
		return PromptContent{Text: prompt.Text}, nil
	}
	content := prompt.Message.Content
	textOnly := true
	for _, part := range content {
		if _, ok := asText(part); !ok {
			textOnly = false
			break
		}
	}
	if textOnly {
		texts := make([]string, 0, len(content))
		for _, part := range content {
			t, _ := asText(part)
			texts = append(texts, t)
		}
		return PromptContent{Text: strings.Join(texts, "\n\n")}, nil
	}
	parts := make([]PromptContentPart, 0, len(content))
	for _, part := range content {
		if t, ok := asText(part); ok {
			parts = append(parts, PromptContentPart{Type: "text", Text: t})
			continue
		}
		var (
			img PromptContentPart
			err error
		)
		switch p := part.(type) {
		case types.ImageContent:
			img, err = imagePartFromImageContent(p)
		case *types.ImageContent:
			img, err = imagePartFromImageContent(*p)
		case types.FileContent:
			img, err = imagePartFromFileContent(p)
		case *types.FileContent:
			img, err = imagePartFromFileContent(*p)
		default:
			err = unsupportedUserContent("content parts other than text, image, or file parts")
		}
		if err != nil {
			return PromptContent{}, err
		}
		parts = append(parts, img)
	}
	return PromptContent{Parts: parts}, nil
}

func asText(part types.ContentPart) (string, bool) {
	switch p := part.(type) {
	case types.TextContent:
		return p.Text, true
	case *types.TextContent:
		return p.Text, true
	}
	return "", false
}

// inlineImage is the result of resolving image data: base64 payload plus the
// media type when it was embedded in a data: URL.
type inlineImage struct {
	data      string
	mediaType string
}

func imagePartFromImageContent(p types.ImageContent) (PromptContentPart, error) {
	mediaType := p.MimeType
	if mediaType == "" {
		mediaType = "image"
	}
	var (
		img inlineImage
		err error
	)
	if len(p.Image) > 0 {
		img = inlineImage{data: base64.StdEncoding.EncodeToString(p.Image)}
	} else if p.URL != "" {
		img, err = inlineImageFromString(p.URL)
	} else {
		img = inlineImage{}
	}
	if err != nil {
		return PromptContentPart{}, err
	}
	return resolveImage(img, mediaType)
}

func imagePartFromFileContent(p types.FileContent) (PromptContentPart, error) {
	mediaType := firstNonEmpty(p.MediaType, p.MimeType, p.FileData.MediaType)
	if topLevelMediaType(mediaType) != "image" {
		return PromptContentPart{}, unsupportedUserContent(fmt.Sprintf("files of media type '%s'", mediaType))
	}
	img, err := inlineImageFromFile(p)
	if err != nil {
		return PromptContentPart{}, err
	}
	return resolveImage(img, mediaType)
}

func inlineImageFromFile(p types.FileContent) (inlineImage, error) {
	if !p.FileData.IsZero() {
		fd := p.FileData
		switch fd.Type {
		case types.FileDataTypeData:
			if fd.DataString != "" {
				return inlineImage{data: fd.DataString}, nil
			}
			return inlineImage{data: base64.StdEncoding.EncodeToString(fd.Data)}, nil
		case types.FileDataTypeURL:
			return inlineImageFromDataURL(fd.URL)
		case types.FileDataTypeReference:
			return inlineImage{}, unsupportedUserContent("provider references")
		case types.FileDataTypeText:
			return inlineImage{}, unsupportedUserContent("text data for images")
		default:
			return inlineImage{}, unsupportedUserContent("this image data")
		}
	}
	switch {
	case len(p.Data) > 0:
		return inlineImage{data: base64.StdEncoding.EncodeToString(p.Data)}, nil
	case p.URL != "":
		return inlineImageFromDataURL(p.URL)
	case p.Reference != "":
		return inlineImage{}, unsupportedUserContent("provider references")
	case p.Text != "":
		return inlineImage{}, unsupportedUserContent("text data for images")
	}
	return inlineImage{}, nil
}

var urlSchemeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*:`)

// inlineImageFromString mirrors the TS string branch: a string that parses as
// a URL must be a data: URL, anything else is raw base64.
func inlineImageFromString(s string) (inlineImage, error) {
	if urlSchemeRE.MatchString(s) {
		return inlineImageFromDataURL(s)
	}
	return inlineImage{data: s}, nil
}

func inlineImageFromDataURL(raw string) (inlineImage, error) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "data") {
		return inlineImage{}, unsupportedUserContent("remote image URLs")
	}
	header, data, ok := strings.Cut(raw[len("data:"):], ",")
	if !ok || !strings.HasSuffix(header, ";base64") {
		return inlineImage{}, unsupportedUserContent("data URLs that are not base64 encoded")
	}
	mediaType, _, _ := strings.Cut(header, ";")
	return inlineImage{data: data, mediaType: mediaType}, nil
}

func resolveImage(img inlineImage, mediaType string) (PromptContentPart, error) {
	resolved := ""
	if isFullMediaType(mediaType) {
		resolved = mediaType
	} else if img.mediaType != "" {
		resolved = img.mediaType
	} else if decoded, err := types.DecodeFileDataString(img.data); err == nil {
		if detected, ok := fileutil.DetectMediaTypeSignature(decoded, "image"); ok {
			resolved = detected
		}
	}
	if resolved == "" {
		return PromptContentPart{}, unsupportedUserContent("images whose media type cannot be detected")
	}
	return PromptContentPart{Type: "image", Data: img.data, MediaType: resolved}, nil
}

func topLevelMediaType(mediaType string) string {
	top, _, _ := strings.Cut(mediaType, "/")
	return top
}

func isFullMediaType(mediaType string) bool {
	_, sub, ok := strings.Cut(mediaType, "/")
	return ok && sub != "" && sub != "*"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func unsupportedUserContent(description string) error {
	return harness.NewCapabilityUnsupportedError(
		fmt.Sprintf("The harness layer does not support %s in user messages. Pass text parts and inline images as bytes, base64 strings, or data URLs.", description),
		"", nil)
}
