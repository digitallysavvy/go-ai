package imageutil

import (
	"encoding/base64"
	"fmt"
)

// EncodeToBase64 converts image bytes to a base64 string.
// This function is used for providers that accept raw base64 (e.g., Alibaba).
//
// Example:
//
//	data := []byte{0xFF, 0xD8, 0xFF}
//	encoded := EncodeToBase64(data) // Returns: "/9j/"
func EncodeToBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// ConvertToDataURI converts image bytes to a data URI string.
// This function is used for providers that accept data URIs (e.g., FAL).
//
// Format: data:<mimeType>;base64,<base64Data>
//
// Example:
//
//	data := []byte{0x89, 0x50, 0x4E, 0x47}
//	uri := ConvertToDataURI(data, "image/png")
//	// Returns: "data:image/png;base64,iVBORw=="
func ConvertToDataURI(data []byte, mimeType string) string {
	encoded := EncodeToBase64(data)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)
}
