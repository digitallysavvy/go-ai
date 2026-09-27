package fileutil

import "testing"

// Ported from ai/packages/provider-utils/src/detect-media-type.test.ts
// (detectMediaType signature matching).

func TestDetectMediaTypeSignatureGIF(t *testing.T) {
	if mt, ok := DetectMediaTypeSignature([]byte{0x47, 0x49, 0x46, 0x38, 0x37, 0x61, 0x00}, "image"); !ok || mt != "image/gif" {
		t.Fatalf("GIF87a: got %q, %v", mt, ok)
	}
	if mt, ok := DetectMediaTypeSignature([]byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x00}, "image"); !ok || mt != "image/gif" {
		t.Fatalf("GIF89a: got %q, %v", mt, ok)
	}
	// "should not detect text that only starts with GIF"
	if _, ok := DetectMediaTypeSignature([]byte("GIF is not always an image"), "image"); ok {
		t.Fatal("expected no match for plain text starting with GIF")
	}
}

func TestDetectMediaTypeSignaturePNGJPEG(t *testing.T) {
	if mt, ok := DetectMediaTypeSignature([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a}, "image"); !ok || mt != "image/png" {
		t.Fatalf("PNG: got %q, %v", mt, ok)
	}
	if mt, ok := DetectMediaTypeSignature([]byte{0xff, 0xd8, 0xff, 0xe0}, "image"); !ok || mt != "image/jpeg" {
		t.Fatalf("JPEG: got %q, %v", mt, ok)
	}
}

func TestDetectMediaTypeSignatureWebP(t *testing.T) {
	webp := []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50}
	if mt, ok := DetectMediaTypeSignature(webp, "image"); !ok || mt != "image/webp" {
		t.Fatalf("WebP: got %q, %v", mt, ok)
	}
	// RIFF....WAVE must not be detected as WebP.
	wav := []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x41, 0x56, 0x45}
	if _, ok := DetectMediaTypeSignature(wav, "image"); ok {
		t.Fatal("expected RIFF/WAVE not to match image signatures")
	}
}

func TestDetectMediaTypeSignatureBMP(t *testing.T) {
	bmp := []byte{0x42, 0x4d, 0x46, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if mt, ok := DetectMediaTypeSignature(bmp, "image"); !ok || mt != "image/bmp" {
		t.Fatalf("BMP: got %q, %v", mt, ok)
	}
	// "should not detect text that only starts with BM"
	if _, ok := DetectMediaTypeSignature([]byte("BM this is just a message, not a bitmap"), "image"); ok {
		t.Fatal("expected no match for plain text starting with BM")
	}
}

func TestDetectMediaTypeSignatureTIFF(t *testing.T) {
	if mt, ok := DetectMediaTypeSignature([]byte{0x49, 0x49, 0x2a, 0x00}, "image"); !ok || mt != "image/tiff" {
		t.Fatalf("TIFF little endian: got %q, %v", mt, ok)
	}
	if mt, ok := DetectMediaTypeSignature([]byte{0x4d, 0x4d, 0x00, 0x2a}, "image"); !ok || mt != "image/tiff" {
		t.Fatalf("TIFF big endian: got %q, %v", mt, ok)
	}
}

func TestDetectMediaTypeSignatureAVIFHEIC(t *testing.T) {
	for _, boxSize := range []byte{0x1c, 0x20} {
		avif := []byte{0x00, 0x00, 0x00, boxSize, 0x66, 0x74, 0x79, 0x70, 0x61, 0x76, 0x69, 0x66}
		if mt, ok := DetectMediaTypeSignature(avif, "image"); !ok || mt != "image/avif" {
			t.Fatalf("AVIF (box=%#x): got %q, %v", boxSize, mt, ok)
		}
		if mt, ok := DetectMediaTypeSignature(avif, ""); !ok || mt != "image/avif" {
			t.Fatalf("AVIF generic (box=%#x): got %q, %v", boxSize, mt, ok)
		}

		heic := []byte{0x00, 0x00, 0x00, boxSize, 0x66, 0x74, 0x79, 0x70, 0x68, 0x65, 0x69, 0x63}
		if mt, ok := DetectMediaTypeSignature(heic, "image"); !ok || mt != "image/heic" {
			t.Fatalf("HEIC (box=%#x): got %q, %v", boxSize, mt, ok)
		}
		if mt, ok := DetectMediaTypeSignature(heic, ""); !ok || mt != "image/heic" {
			t.Fatalf("HEIC generic (box=%#x): got %q, %v", boxSize, mt, ok)
		}
	}
}

func TestDetectMediaTypeSignatureMP3(t *testing.T) {
	for _, prefix := range [][2]byte{{0xff, 0xfb}, {0xff, 0xfa}, {0xff, 0xf3}, {0xff, 0xf2}, {0xff, 0xe3}, {0xff, 0xe2}} {
		if mt, ok := DetectMediaTypeSignature([]byte{prefix[0], prefix[1]}, "audio"); !ok || mt != "audio/mpeg" {
			t.Fatalf("MP3 %v: got %q, %v", prefix, mt, ok)
		}
	}
}

func TestDetectMediaTypeSignatureMP3WithID3(t *testing.T) {
	mp3WithID3 := []byte{
		0x49, 0x44, 0x33, // 'ID3'
		0x03, 0x00, // version
		0x00,                   // flags
		0x00, 0x00, 0x00, 0x0a, // synchsafe size (10)
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 10 bytes of tag data
		0xff, 0xfb, 0x00, 0x00, // MP3 frame header
	}
	if mt, ok := DetectMediaTypeSignature(mp3WithID3, "audio"); !ok || mt != "audio/mpeg" {
		t.Fatalf("ID3-tagged MP3: got %q, %v", mt, ok)
	}
}

// buildID3MP3 constructs an ID3v2-tagged MP3 with a tagBody-byte tag and the
// frame sync placed immediately after the tag, mirroring the TS test's
// buildID3Mp3 helper.
func buildID3MP3(tagBody int) []byte {
	b := make([]byte, 10+tagBody+2)
	b[0], b[1], b[2] = 0x49, 0x44, 0x33
	b[6] = byte((tagBody >> 21) & 0x7f)
	b[7] = byte((tagBody >> 14) & 0x7f)
	b[8] = byte((tagBody >> 7) & 0x7f)
	b[9] = byte(tagBody & 0x7f)
	b[10+tagBody] = 0xff
	b[10+tagBody+1] = 0xfb
	return b
}

func TestDetectMediaTypeSignatureID3ScanBoundary(t *testing.T) {
	atLimit := buildID3MP3(MaxID3TagBytes)
	if mt, ok := DetectMediaTypeSignature(atLimit, "audio"); !ok || mt != "audio/mpeg" {
		t.Fatalf("ID3 tag at scan limit: got %q, %v", mt, ok)
	}

	overLimit := buildID3MP3(MaxID3TagBytes + 1)
	if _, ok := DetectMediaTypeSignature(overLimit, "audio"); ok {
		t.Fatal("expected no match for an ID3 tag exceeding the scan limit")
	}
}

func TestDetectMediaTypeSignatureWAV(t *testing.T) {
	wav := []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x41, 0x56, 0x45}
	if mt, ok := DetectMediaTypeSignature(wav, "audio"); !ok || mt != "audio/wav" {
		t.Fatalf("WAV: got %q, %v", mt, ok)
	}
	webp := []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50}
	if _, ok := DetectMediaTypeSignature(webp, "audio"); ok {
		t.Fatal("expected RIFF/WEBP not to match audio signatures")
	}
}

func TestDetectMediaTypeSignatureOGGFLAC(t *testing.T) {
	if mt, ok := DetectMediaTypeSignature([]byte{0x4f, 0x67, 0x67, 0x53}, "audio"); !ok || mt != "audio/ogg" {
		t.Fatalf("OGG: got %q, %v", mt, ok)
	}
	if mt, ok := DetectMediaTypeSignature([]byte{0x66, 0x4c, 0x61, 0x43}, "audio"); !ok || mt != "audio/flac" {
		t.Fatalf("FLAC: got %q, %v", mt, ok)
	}
}

func TestDetectMediaTypeSignatureAAC(t *testing.T) {
	// ADTS AAC (MPEG-4/MPEG-2, with/without CRC) - not recognized by net/http.
	for _, prefix := range [][2]byte{{0xff, 0xf0}, {0xff, 0xf1}, {0xff, 0xf8}, {0xff, 0xf9}} {
		data := []byte{prefix[0], prefix[1], 0x00, 0x00}
		if mt, ok := DetectMediaTypeSignature(data, "audio"); !ok || mt != "audio/aac" {
			t.Fatalf("ADTS AAC %v: got %q, %v", prefix, mt, ok)
		}
	}
	if mt, ok := DetectMediaTypeSignature([]byte{0x40, 0x15, 0x00, 0x00}, "audio"); !ok || mt != "audio/aac" {
		t.Fatalf("LOAS/LATM AAC: got %q, %v", mt, ok)
	}
}

func TestDetectMediaTypeSignatureMP4(t *testing.T) {
	mp4 := []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 0x69, 0x73, 0x6f, 0x6d}
	if mt, ok := DetectMediaTypeSignature(mp4, "video"); !ok || mt != "video/mp4" {
		t.Fatalf("video/mp4: got %q, %v", mt, ok)
	}
	// Same bytes, but requested as audio: an MP4 container holding audio
	// (e.g. M4A) must be reported as audio/mp4, not dropped.
	if mt, ok := DetectMediaTypeSignature(mp4, "audio"); !ok || mt != "audio/mp4" {
		t.Fatalf("audio/mp4 (M4A): got %q, %v", mt, ok)
	}
	// Generic (no topLevelType) scan can't disambiguate MP4 audio vs video,
	// and reports it as video/mp4, matching the TS behavior/comment.
	if mt, ok := DetectMediaTypeSignature(mp4, ""); !ok || mt != "video/mp4" {
		t.Fatalf("generic mp4: got %q, %v", mt, ok)
	}
}

func TestDetectMediaTypeSignatureWebM(t *testing.T) {
	webm := []byte{0x1a, 0x45, 0xdf, 0xa3}
	if mt, ok := DetectMediaTypeSignature(webm, "video"); !ok || mt != "video/webm" {
		t.Fatalf("video/webm: got %q, %v", mt, ok)
	}
	if mt, ok := DetectMediaTypeSignature(webm, "audio"); !ok || mt != "audio/webm" {
		t.Fatalf("audio/webm: got %q, %v", mt, ok)
	}
}

func TestDetectMediaTypeSignatureDocument(t *testing.T) {
	if mt, ok := DetectMediaTypeSignature([]byte{0x25, 0x50, 0x44, 0x46, 0x2d}, "application"); !ok || mt != "application/pdf" {
		t.Fatalf("PDF: got %q, %v", mt, ok)
	}
}

func TestDetectMediaTypeSignatureErrorCases(t *testing.T) {
	if _, ok := DetectMediaTypeSignature([]byte{0x00, 0x01, 0x02, 0x03}, "image"); ok {
		t.Fatal("expected no match for unknown image bytes")
	}
	if _, ok := DetectMediaTypeSignature([]byte{}, "image"); ok {
		t.Fatal("expected no match for empty data")
	}
	if _, ok := DetectMediaTypeSignature([]byte{0x89, 0x50}, "image"); ok {
		t.Fatal("expected no match for data shorter than the signature")
	}
	if _, ok := DetectMediaTypeSignature([]byte{0x00}, "text"); ok {
		t.Fatal("expected no match for an unsupported top-level segment")
	}
}

// Note: DetectMediaType (the general, non-signature-scoped detector used by
// image/generic file handling) is not wired to DetectMediaTypeSignature here;
// that wiring belongs to whichever slice owns pkg/internal/fileutil/mediatype.go
// generally. This package only needs the topLevelType-scoped
// DetectMediaTypeSignature("audio", ...) call used by transcription.
