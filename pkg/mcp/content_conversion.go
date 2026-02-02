package mcp

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ConvertMCPContentToAISDK converts MCP tool result content to AI SDK content parts
// This properly handles image content to prevent token explosions (200K+ tokens)
func ConvertMCPContentToAISDK(mcpContent []ToolResultContent) ([]types.ContentPart, error) {
	if len(mcpContent) == 0 {
		return nil, nil
	}

	aiContent := make([]types.ContentPart, 0, len(mcpContent))

	for _, content := range mcpContent {
		part, err := convertSingleContent(content)
		if err != nil {
			return nil, fmt.Errorf("failed to convert content item: %w", err)
		}
		if part != nil {
			aiContent = append(aiContent, part)
		}
	}

	return aiContent, nil
}

// convertSingleContent converts a single MCP content item to AI SDK format
func convertSingleContent(item ToolResultContent) (types.ContentPart, error) {
	switch item.Type {
	case "text":
		return convertMCPTextToAISDK(item), nil
	case "image":
		return convertMCPImageToAISDK(item)
	case "resource":
		return convertMCPResourceToAISDK(item), nil
	default:
		// Unknown type, treat as text
		return types.TextContent{
			Text: fmt.Sprintf("Unknown content type: %s", item.Type),
		}, nil
	}
}

// convertMCPTextToAISDK converts MCP text content to AI SDK TextContent
func convertMCPTextToAISDK(item ToolResultContent) types.TextContent {
	return types.TextContent{
		Text: item.Text,
	}
}

// convertMCPImageToAISDK converts MCP image content to AI SDK ImageContent
// This is the critical fix that prevents 200K+ token explosions
func convertMCPImageToAISDK(item ToolResultContent) (types.ImageContent, error) {
	// Validate MIME type
	if item.MimeType == "" {
		return types.ImageContent{}, fmt.Errorf("missing MIME type for image content")
	}

	// Validate image data
	if item.Data == "" {
		return types.ImageContent{}, fmt.Errorf("empty image data")
	}

	// Check if data is a URL (HTTP/HTTPS)
	if strings.HasPrefix(item.Data, "http://") || strings.HasPrefix(item.Data, "https://") {
		return types.ImageContent{
			URL:      item.Data,
			MimeType: item.MimeType,
			Image:    nil, // URL-based image, no bytes needed
		}, nil
	}

	// Check if data is a data URL (data:image/png;base64,...)
	if strings.HasPrefix(item.Data, "data:") {
		// Extract base64 data from data URL
		parts := strings.SplitN(item.Data, ",", 2)
		if len(parts) != 2 {
			return types.ImageContent{}, fmt.Errorf("invalid data URL format")
		}

		// Decode base64 to bytes
		imageBytes, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return types.ImageContent{}, fmt.Errorf("failed to decode base64 image data: %w", err)
		}

		return types.ImageContent{
			Image:    imageBytes,
			MimeType: item.MimeType,
		}, nil
	}

	// Assume raw base64 data
	imageBytes, err := base64.StdEncoding.DecodeString(item.Data)
	if err != nil {
		return types.ImageContent{}, fmt.Errorf("failed to decode base64 image data: %w", err)
	}

	return types.ImageContent{
		Image:    imageBytes,
		MimeType: item.MimeType,
	}, nil
}

// convertMCPResourceToAISDK converts MCP resource content to AI SDK content
func convertMCPResourceToAISDK(item ToolResultContent) types.ContentPart {
	// If resource is an image, try to treat it as an image URL
	if strings.HasPrefix(item.MimeType, "image/") && item.URI != "" {
		return types.ImageContent{
			URL:      item.URI,
			MimeType: item.MimeType,
			Image:    nil, // URL-based image
		}
	}

	// Otherwise, return as text with URI information
	text := item.URI
	if item.Text != "" {
		text = item.Text
	}

	return types.TextContent{
		Text: text,
	}
}
