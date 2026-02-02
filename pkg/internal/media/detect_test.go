package media

import (
	"testing"
)

func TestDetectVideoMediaType(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{
			name: "MP4 file",
			data: []byte{
				0x00, 0x00, 0x00, 0x20, // size
				0x66, 0x74, 0x79, 0x70, // 'ftyp'
				0x69, 0x73, 0x6F, 0x6D, // 'isom'
			},
			expected: "video/mp4",
		},
		{
			name: "WebM file",
			data: []byte{
				0x1A, 0x45, 0xDF, 0xA3, // EBML header
				0x01, 0x00, 0x00, 0x00,
			},
			expected: "video/webm",
		},
		{
			name: "QuickTime file",
			data: []byte{
				0x00, 0x00, 0x00, 0x14, // size
				0x66, 0x74, 0x79, 0x70, // 'ftyp'
				0x71, 0x74, 0x20, 0x20, // 'qt  '
			},
			expected: "video/quicktime",
		},
		{
			name: "AVI file",
			data: []byte{
				0x52, 0x49, 0x46, 0x46, // 'RIFF'
				0x00, 0x00, 0x00, 0x00, // size
				0x41, 0x56, 0x49, 0x20, // 'AVI '
			},
			expected: "video/x-msvideo",
		},
		{
			name:     "Unknown format (default to MP4)",
			data:     []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B},
			expected: "video/mp4",
		},
		{
			name:     "Too short data (default to MP4)",
			data:     []byte{0x00, 0x01, 0x02},
			expected: "video/mp4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectVideoMediaType(tt.data)
			if result != tt.expected {
				t.Errorf("DetectVideoMediaType() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDetectImageMediaType(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{
			name: "PNG file",
			data: []byte{
				0x89, 0x50, 0x4E, 0x47, // PNG signature
				0x0D, 0x0A, 0x1A, 0x0A,
				0x00, 0x00, 0x00, 0x0D,
			},
			expected: "image/png",
		},
		{
			name: "JPEG file",
			data: []byte{
				0xFF, 0xD8, 0xFF, 0xE0, // JPEG signature
				0x00, 0x10, 0x4A, 0x46,
			},
			expected: "image/jpeg",
		},
		{
			name: "GIF file",
			data: []byte{
				0x47, 0x49, 0x46, 0x38, // 'GIF8'
				0x39, 0x61, 0x00, 0x00,
			},
			expected: "image/gif",
		},
		{
			name: "WebP file",
			data: []byte{
				0x52, 0x49, 0x46, 0x46, // 'RIFF'
				0x00, 0x00, 0x00, 0x00, // size
				0x57, 0x45, 0x42, 0x50, // 'WEBP'
			},
			expected: "image/webp",
		},
		{
			name:     "Unknown format (default to JPEG)",
			data:     []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B},
			expected: "image/jpeg",
		},
		{
			name:     "Too short data (default to JPEG)",
			data:     []byte{0x00, 0x01, 0x02},
			expected: "image/jpeg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectImageMediaType(tt.data)
			if result != tt.expected {
				t.Errorf("DetectImageMediaType() = %v, want %v", result, tt.expected)
			}
		})
	}
}
