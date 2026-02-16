package media

// DetectVideoMediaType detects video format from binary data
// Returns the MIME type based on file signature (magic numbers)
func DetectVideoMediaType(data []byte) string {
	if len(data) < 4 {
		// Default to MP4 if data is too short to detect
		return "video/mp4"
	}

	// Check WebM signature first: 1A 45 DF A3
	// This is the EBML (Extensible Binary Meta Language) header
	if len(data) >= 4 &&
		data[0] == 0x1A && data[1] == 0x45 &&
		data[2] == 0xDF && data[3] == 0xA3 {
		return "video/webm"
	}

	// Check AVI signature: 52 49 46 46 xx xx xx xx 41 56 49 20
	// RIFF....AVI (first 4 bytes: RIFF, bytes 8-11: AVI )
	if len(data) >= 12 &&
		data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 &&
		data[8] == 0x41 && data[9] == 0x56 && data[10] == 0x49 && data[11] == 0x20 {
		return "video/x-msvideo"
	}

	// Check QuickTime signature: 00 00 00 xx 66 74 79 70 71 74
	// QuickTime files have 'ftypqt  ' signature
	if len(data) >= 12 &&
		data[4] == 0x66 && data[5] == 0x74 &&
		data[6] == 0x79 && data[7] == 0x70 &&
		data[8] == 0x71 && data[9] == 0x74 {
		return "video/quicktime"
	}

	// Check MP4 signature last: 00 00 00 xx 66 74 79 70
	// The 'ftyp' box is the standard MP4 file type box
	// This must come after QuickTime check since QuickTime also has ftyp
	if len(data) >= 8 &&
		data[4] == 0x66 && data[5] == 0x74 &&
		data[6] == 0x79 && data[7] == 0x70 {
		return "video/mp4"
	}

	// Default to MP4 if no signature matches
	return "video/mp4"
}

// DetectImageMediaType detects image format from binary data
// Returns the MIME type based on file signature (magic numbers)
func DetectImageMediaType(data []byte) string {
	if len(data) < 3 {
		// Default to JPEG if data is too short to detect
		return "image/jpeg"
	}

	// Check PNG signature: 89 50 4E 47 0D 0A 1A 0A
	if len(data) >= 8 &&
		data[0] == 0x89 && data[1] == 0x50 &&
		data[2] == 0x4E && data[3] == 0x47 &&
		data[4] == 0x0D && data[5] == 0x0A &&
		data[6] == 0x1A && data[7] == 0x0A {
		return "image/png"
	}

	// Check GIF signature: 47 49 46 38 (GIF8)
	// Must check before JPEG because we only need 4 bytes
	if len(data) >= 4 &&
		data[0] == 0x47 && data[1] == 0x49 &&
		data[2] == 0x46 && data[3] == 0x38 {
		return "image/gif"
	}

	// Check JPEG signature: FF D8 FF
	if len(data) >= 3 &&
		data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}

	// Check WebP signature: 52 49 46 46 xx xx xx xx 57 45 42 50
	// RIFF....WEBP
	if len(data) >= 12 &&
		data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 &&
		data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50 {
		return "image/webp"
	}

	// Default to JPEG if no signature matches
	return "image/jpeg"
}
