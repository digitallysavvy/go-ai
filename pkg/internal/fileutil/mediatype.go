package fileutil

import (
	"bytes"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// MediaType represents a detected media type
type MediaType struct {
	// MimeType is the MIME type (e.g., "image/png")
	MimeType string

	// Category is the general category (e.g., "image", "audio", "video", "text")
	Category string

	// Extension is the file extension (e.g., ".png")
	Extension string
}

// DetectMediaType detects the media type from data. It first tries strict
// byte-signature matching (image/audio/video/document formats, including
// AVIF, HEIC and ADTS AAC which net/http does not recognize), then falls back
// to net/http.DetectContentType for everything else.
func DetectMediaType(data []byte) MediaType {
	if mimeType, ok := DetectMediaTypeSignature(data, ""); ok {
		return MediaType{
			MimeType:  mimeType,
			Category:  categoryFromMimeType(mimeType),
			Extension: extensionFromMimeType(mimeType),
		}
	}

	// Use http.DetectContentType which uses the first 512 bytes
	mimeType := http.DetectContentType(data)
	if mimeType == "image/bmp" && looksLikeText(data) {
		// net/http's BMP sniffer only checks the 2-byte "BM" prefix, so plain
		// text beginning with those letters (e.g. "BM this is a message")
		// otherwise misclassifies as image/bmp. Our stricter signature check
		// above already ruled out a real BMP header (it also validates the
		// reserved header bytes), so treat it as text instead.
		mimeType = "text/plain; charset=utf-8"
	}

	return MediaType{
		MimeType:  mimeType,
		Category:  categoryFromMimeType(mimeType),
		Extension: extensionFromMimeType(mimeType),
	}
}

// looksLikeText reports whether data is plausibly readable text: valid UTF-8
// with no control bytes other than common whitespace.
func looksLikeText(data []byte) bool {
	sample := data
	if len(sample) > 512 {
		sample = sample[:512]
	}
	if len(sample) == 0 || !utf8.Valid(sample) {
		return false
	}
	for _, b := range sample {
		switch {
		case b == '\t' || b == '\n' || b == '\r':
			continue
		case b < 0x20 || b == 0x7f:
			return false
		}
	}
	return true
}

// DetectMediaTypeFromFilename detects media type from filename
func DetectMediaTypeFromFilename(filename string) MediaType {
	ext := filepath.Ext(filename)
	mimeType := mime.TypeByExtension(ext)

	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	return MediaType{
		MimeType:  mimeType,
		Category:  categoryFromMimeType(mimeType),
		Extension: ext,
	}
}

// IsImage returns true if the media type is an image
func (m MediaType) IsImage() bool {
	return m.Category == "image"
}

// IsAudio returns true if the media type is audio
func (m MediaType) IsAudio() bool {
	return m.Category == "audio"
}

// IsVideo returns true if the media type is video
func (m MediaType) IsVideo() bool {
	return m.Category == "video"
}

// IsText returns true if the media type is text
func (m MediaType) IsText() bool {
	return m.Category == "text"
}

// categoryFromMimeType extracts the category from a MIME type
func categoryFromMimeType(mimeType string) string {
	parts := strings.Split(mimeType, "/")
	if len(parts) > 0 {
		return strings.TrimSpace(parts[0])
	}
	return "application"
}

// extensionFromMimeType gets the file extension for a MIME type
func extensionFromMimeType(mimeType string) string {
	// Common mappings
	extensions := map[string]string{
		"image/jpeg":       ".jpg",
		"image/png":        ".png",
		"image/gif":        ".gif",
		"image/webp":       ".webp",
		"image/svg+xml":    ".svg",
		"audio/mpeg":       ".mp3",
		"audio/wav":        ".wav",
		"audio/ogg":        ".ogg",
		"audio/webm":       ".webm",
		"video/mp4":        ".mp4",
		"video/webm":       ".webm",
		"video/ogg":        ".ogv",
		"text/plain":       ".txt",
		"text/html":        ".html",
		"text/css":         ".css",
		"text/javascript":  ".js",
		"application/json": ".json",
		"application/pdf":  ".pdf",
	}

	if ext, ok := extensions[mimeType]; ok {
		return ext
	}

	// Try to use mime package
	exts, err := mime.ExtensionsByType(mimeType)
	if err == nil && len(exts) > 0 {
		return exts[0]
	}

	return ""
}

// ValidateMediaType checks if data matches the expected media type
func ValidateMediaType(data []byte, expectedMimeType string) error {
	detected := DetectMediaType(data)

	// Normalize MIME types for comparison
	expected := strings.ToLower(strings.TrimSpace(expectedMimeType))
	actual := strings.ToLower(strings.TrimSpace(detected.MimeType))

	// Handle special cases
	if strings.HasPrefix(expected, actual) || strings.HasPrefix(actual, expected) {
		return nil
	}

	// Check category match as fallback
	expectedCategory := categoryFromMimeType(expected)
	if expectedCategory == detected.Category {
		return nil
	}

	return fmt.Errorf("media type mismatch: expected %s but got %s", expectedMimeType, detected.MimeType)
}

// SplitDataURL splits a data URL into its components
// Example: "data:image/png;base64,iVBORw0KG..." -> (image/png, base64, iVBORw0KG...)
func SplitDataURL(dataURL string) (mimeType string, encoding string, data string, err error) {
	if !strings.HasPrefix(dataURL, "data:") {
		return "", "", "", fmt.Errorf("invalid data URL: missing 'data:' prefix")
	}

	// Remove "data:" prefix
	dataURL = dataURL[5:]

	// Split on first comma
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return "", "", "", fmt.Errorf("invalid data URL: missing comma separator")
	}

	metadata := parts[0]
	data = parts[1]

	// Parse metadata (e.g., "image/png;base64")
	metaParts := strings.Split(metadata, ";")
	if len(metaParts) > 0 {
		mimeType = metaParts[0]
	}

	if len(metaParts) > 1 {
		encoding = metaParts[1]
	}

	if mimeType == "" {
		mimeType = "text/plain"
	}

	if encoding == "" {
		encoding = "charset=US-ASCII"
	}

	return mimeType, encoding, data, nil
}

// CreateDataURL creates a data URL from mime type and data
func CreateDataURL(mimeType string, data []byte) string {
	encoded := bytes.NewBuffer(nil)
	encoded.WriteString("data:")
	encoded.WriteString(mimeType)
	encoded.WriteString(";base64,")
	encoded.WriteString(string(data)) // Assume data is already base64 encoded
	return encoded.String()
}
