package mcp

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test converting MCP text content to AI SDK format
func TestConvertMCPTextToAISDK(t *testing.T) {
	mcpContent := ToolResultContent{
		Type: "text",
		Text: "Hello, world!",
	}

	result := convertMCPTextToAISDK(mcpContent)

	assert.Equal(t, "Hello, world!", result.Text)
	assert.Equal(t, "text", result.ContentType())
}

// Test converting MCP image content with base64 data
func TestConvertMCPImageToAISDK_Base64(t *testing.T) {
	// Create a small 1x1 PNG image in base64
	imageData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

	mcpContent := ToolResultContent{
		Type:     "image",
		Data:     imageData,
		MimeType: "image/png",
	}

	result, err := convertMCPImageToAISDK(mcpContent)
	require.NoError(t, err)

	assert.Equal(t, "image/png", result.MimeType)
	assert.NotNil(t, result.Image)
	assert.Greater(t, len(result.Image), 0)

	// Verify it's valid base64 decode
	expectedBytes, _ := base64.StdEncoding.DecodeString(imageData)
	assert.Equal(t, expectedBytes, result.Image)
}

// Test converting MCP image content with data URL
func TestConvertMCPImageToAISDK_DataURL(t *testing.T) {
	imageData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	dataURL := "data:image/png;base64," + imageData

	mcpContent := ToolResultContent{
		Type:     "image",
		Data:     dataURL,
		MimeType: "image/png",
	}

	result, err := convertMCPImageToAISDK(mcpContent)
	require.NoError(t, err)

	assert.Equal(t, "image/png", result.MimeType)
	assert.NotNil(t, result.Image)

	// Verify it's correctly decoded
	expectedBytes, _ := base64.StdEncoding.DecodeString(imageData)
	assert.Equal(t, expectedBytes, result.Image)
}

// Test converting MCP image content with HTTP URL
func TestConvertMCPImageToAISDK_HttpURL(t *testing.T) {
	imageURL := "https://example.com/image.png"

	mcpContent := ToolResultContent{
		Type:     "image",
		Data:     imageURL,
		MimeType: "image/png",
	}

	result, err := convertMCPImageToAISDK(mcpContent)
	require.NoError(t, err)

	assert.Equal(t, "image/png", result.MimeType)
	assert.Equal(t, imageURL, result.URL)
	assert.Nil(t, result.Image) // URL-based images don't have bytes
}

// Test converting MCP image content with HTTPS URL
func TestConvertMCPImageToAISDK_HttpsURL(t *testing.T) {
	imageURL := "http://example.com/image.jpg"

	mcpContent := ToolResultContent{
		Type:     "image",
		Data:     imageURL,
		MimeType: "image/jpeg",
	}

	result, err := convertMCPImageToAISDK(mcpContent)
	require.NoError(t, err)

	assert.Equal(t, "image/jpeg", result.MimeType)
	assert.Equal(t, imageURL, result.URL)
	assert.Nil(t, result.Image)
}

// Test error handling for missing MIME type
func TestConvertMCPImageToAISDK_MissingMimeType(t *testing.T) {
	mcpContent := ToolResultContent{
		Type:     "image",
		Data:     "base64data",
		MimeType: "", // Missing MIME type
	}

	_, err := convertMCPImageToAISDK(mcpContent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing MIME type")
}

// Test error handling for empty image data
func TestConvertMCPImageToAISDK_EmptyData(t *testing.T) {
	mcpContent := ToolResultContent{
		Type:     "image",
		Data:     "", // Empty data
		MimeType: "image/png",
	}

	_, err := convertMCPImageToAISDK(mcpContent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty image data")
}

// Test error handling for invalid base64
func TestConvertMCPImageToAISDK_InvalidBase64(t *testing.T) {
	mcpContent := ToolResultContent{
		Type:     "image",
		Data:     "not-valid-base64!!!",
		MimeType: "image/png",
	}

	_, err := convertMCPImageToAISDK(mcpContent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode base64")
}

// Test error handling for invalid data URL format
func TestConvertMCPImageToAISDK_InvalidDataURL(t *testing.T) {
	mcpContent := ToolResultContent{
		Type:     "image",
		Data:     "data:image/png", // Missing comma separator
		MimeType: "image/png",
	}

	_, err := convertMCPImageToAISDK(mcpContent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid data URL format")
}

// Test converting MCP resource content with image MIME type
func TestConvertMCPResourceToAISDK_ImageResource(t *testing.T) {
	mcpContent := ToolResultContent{
		Type:     "resource",
		URI:      "https://example.com/chart.png",
		MimeType: "image/png",
	}

	result := convertMCPResourceToAISDK(mcpContent)

	// Should be converted to ImageContent
	imageContent, ok := result.(types.ImageContent)
	require.True(t, ok)
	assert.Equal(t, "https://example.com/chart.png", imageContent.URL)
	assert.Equal(t, "image/png", imageContent.MimeType)
}

// Test converting MCP resource content with text
func TestConvertMCPResourceToAISDK_TextResource(t *testing.T) {
	mcpContent := ToolResultContent{
		Type:     "resource",
		URI:      "file:///path/to/file.txt",
		Text:     "File content here",
		MimeType: "text/plain",
	}

	result := convertMCPResourceToAISDK(mcpContent)

	// Should be converted to TextContent
	textContent, ok := result.(types.TextContent)
	require.True(t, ok)
	assert.Equal(t, "File content here", textContent.Text)
}

// Test converting MCP resource content with only URI
func TestConvertMCPResourceToAISDK_URIOnly(t *testing.T) {
	mcpContent := ToolResultContent{
		Type:     "resource",
		URI:      "https://example.com/document.pdf",
		MimeType: "application/pdf",
	}

	result := convertMCPResourceToAISDK(mcpContent)

	// Should be converted to TextContent with URI
	textContent, ok := result.(types.TextContent)
	require.True(t, ok)
	assert.Equal(t, "https://example.com/document.pdf", textContent.Text)
}

// Test converting mixed content (text + image)
func TestConvertMCPContentToAISDK_MixedContent(t *testing.T) {
	imageData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

	mcpContents := []ToolResultContent{
		{
			Type: "text",
			Text: "Here is the chart:",
		},
		{
			Type:     "image",
			Data:     imageData,
			MimeType: "image/png",
		},
		{
			Type: "text",
			Text: "The chart shows an upward trend.",
		},
	}

	results, err := ConvertMCPContentToAISDK(mcpContents)
	require.NoError(t, err)
	require.Len(t, results, 3)

	// Check first item is text
	textContent1, ok := results[0].(types.TextContent)
	require.True(t, ok)
	assert.Equal(t, "Here is the chart:", textContent1.Text)

	// Check second item is image
	imageContent, ok := results[1].(types.ImageContent)
	require.True(t, ok)
	assert.Equal(t, "image/png", imageContent.MimeType)
	assert.NotNil(t, imageContent.Image)

	// Check third item is text
	textContent2, ok := results[2].(types.TextContent)
	require.True(t, ok)
	assert.Equal(t, "The chart shows an upward trend.", textContent2.Text)
}

// Test converting empty content array
func TestConvertMCPContentToAISDK_EmptyArray(t *testing.T) {
	results, err := ConvertMCPContentToAISDK([]ToolResultContent{})
	require.NoError(t, err)
	assert.Nil(t, results)
}

// Test converting nil content array
func TestConvertMCPContentToAISDK_NilArray(t *testing.T) {
	results, err := ConvertMCPContentToAISDK(nil)
	require.NoError(t, err)
	assert.Nil(t, results)
}

// Test converting unknown content type
func TestConvertMCPContentToAISDK_UnknownType(t *testing.T) {
	mcpContents := []ToolResultContent{
		{
			Type: "unknown-type",
			Text: "Some data",
		},
	}

	results, err := ConvertMCPContentToAISDK(mcpContents)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Should fall back to text content
	textContent, ok := results[0].(types.TextContent)
	require.True(t, ok)
	assert.True(t, strings.Contains(textContent.Text, "Unknown content type"))
}

// Test that image conversion prevents token explosion
func TestConvertMCPImageToAISDK_PreventTokenExplosion(t *testing.T) {
	// Create a reasonably sized base64 image (this would cause ~200K tokens if treated as text)
	// Use valid base64 by encoding actual data
	largeImageBytes := make([]byte, 50000) // 50KB image
	for i := range largeImageBytes {
		largeImageBytes[i] = byte(i % 256)
	}
	imageData := base64.StdEncoding.EncodeToString(largeImageBytes)

	mcpContent := ToolResultContent{
		Type:     "image",
		Data:     imageData,
		MimeType: "image/png",
	}

	result, err := convertMCPImageToAISDK(mcpContent)
	require.NoError(t, err)

	// Image data should be stored as bytes, not text
	assert.NotNil(t, result.Image)
	assert.Equal(t, "image/png", result.MimeType)

	// The key point: image bytes are not converted to text/JSON
	// This prevents the 200K+ token explosion
	assert.Equal(t, largeImageBytes, result.Image)

	// If this were treated as text, the base64 string would be ~66KB
	// which would translate to ~200K+ tokens when sent to the model
	// By storing as bytes, the model handles it as native image content
}

// Benchmark to verify performance of image conversion
func BenchmarkConvertMCPImageToAISDK_Base64(b *testing.B) {
	imageData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	mcpContent := ToolResultContent{
		Type:     "image",
		Data:     imageData,
		MimeType: "image/png",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = convertMCPImageToAISDK(mcpContent)
	}
}

// Benchmark mixed content conversion
func BenchmarkConvertMCPContentToAISDK_MixedContent(b *testing.B) {
	imageData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	mcpContents := []ToolResultContent{
		{Type: "text", Text: "Here is the chart:"},
		{Type: "image", Data: imageData, MimeType: "image/png"},
		{Type: "text", Text: "Analysis complete."},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ConvertMCPContentToAISDK(mcpContents)
	}
}
