package imageutil

import (
	"testing"
)

func TestEncodeToBase64(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "Empty data",
			data: []byte{},
			want: "",
		},
		{
			name: "Simple text data",
			data: []byte("hello"),
			want: "aGVsbG8=",
		},
		{
			name: "JPEG header",
			data: []byte{0xFF, 0xD8, 0xFF},
			want: "/9j/",
		},
		{
			name: "PNG header",
			data: []byte{0x89, 0x50, 0x4E, 0x47},
			want: "iVBORw==",
		},
		{
			name: "WebP header",
			data: []byte{0x52, 0x49, 0x46, 0x46},
			want: "UklGRg==",
		},
		{
			name: "Single byte",
			data: []byte{0xFF},
			want: "/w==",
		},
		{
			name: "Two bytes",
			data: []byte{0xFF, 0xFF},
			want: "//8=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EncodeToBase64(tt.data)
			if got != tt.want {
				t.Errorf("EncodeToBase64() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConvertToDataURI(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		mimeType string
		want     string
	}{
		{
			name:     "JPEG image",
			data:     []byte{0xFF, 0xD8, 0xFF},
			mimeType: "image/jpeg",
			want:     "data:image/jpeg;base64,/9j/",
		},
		{
			name:     "PNG image",
			data:     []byte{0x89, 0x50, 0x4E, 0x47},
			mimeType: "image/png",
			want:     "data:image/png;base64,iVBORw==",
		},
		{
			name:     "WebP image",
			data:     []byte{0x52, 0x49, 0x46, 0x46},
			mimeType: "image/webp",
			want:     "data:image/webp;base64,UklGRg==",
		},
		{
			name:     "GIF image",
			data:     []byte{0x47, 0x49, 0x46, 0x38},
			mimeType: "image/gif",
			want:     "data:image/gif;base64,R0lGOA==",
		},
		{
			name:     "Empty data",
			data:     []byte{},
			mimeType: "image/png",
			want:     "data:image/png;base64,",
		},
		{
			name:     "Custom mime type",
			data:     []byte("test"),
			mimeType: "image/custom",
			want:     "data:image/custom;base64,dGVzdA==",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertToDataURI(tt.data, tt.mimeType)
			if got != tt.want {
				t.Errorf("ConvertToDataURI() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConvertToDataURIFormat(t *testing.T) {
	data := []byte("test data")
	mimeType := "image/png"
	result := ConvertToDataURI(data, mimeType)

	// Verify it starts with "data:"
	if len(result) < 5 || result[:5] != "data:" {
		t.Errorf("ConvertToDataURI() should start with 'data:', got: %q", result)
	}

	// Verify it contains the mime type
	if len(result) < len("data:"+mimeType) {
		t.Errorf("ConvertToDataURI() too short to contain mime type")
	}

	// Verify it contains ";base64,"
	expectedPrefix := "data:" + mimeType + ";base64,"
	if len(result) < len(expectedPrefix) || result[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("ConvertToDataURI() should start with %q, got: %q", expectedPrefix, result)
	}
}

func BenchmarkEncodeToBase64(b *testing.B) {
	// Simulate a 1MB image
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EncodeToBase64(data)
	}
}

func BenchmarkConvertToDataURI(b *testing.B) {
	// Simulate a 1MB image
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ConvertToDataURI(data, "image/jpeg")
	}
}
