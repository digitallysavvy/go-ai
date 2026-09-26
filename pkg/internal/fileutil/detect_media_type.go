package fileutil

// This file ports the TypeScript AI SDK's byte-signature media type detector
// (provider-utils/src/detect-media-type.ts) so Go providers can identify
// image/audio/video/document formats without relying solely on
// net/http.DetectContentType, whose short prefix checks both miss newer
// container formats (AVIF/HEIC ftyp boxes, ADTS AAC frame sync) and produce
// false positives (plain text starting with "BM" sniffs as image/bmp).

// mediaTypeSignature is a byte-prefix match rule. A byteWildcard entry in
// bytesPrefix matches any byte at that position (used for variable fields
// such as RIFF/ftyp box sizes).
type mediaTypeSignature struct {
	mediaType   string
	bytesPrefix []int
}

// byteWildcard marks a position in bytesPrefix that matches any byte.
const byteWildcard = -1

var imageMediaTypeSignatures = []mediaTypeSignature{
	{"image/gif", []int{0x47, 0x49, 0x46, 0x38, 0x37, 0x61}}, // GIF87a
	{"image/gif", []int{0x47, 0x49, 0x46, 0x38, 0x39, 0x61}}, // GIF89a
	{"image/png", []int{0x89, 0x50, 0x4e, 0x47}},
	{"image/jpeg", []int{0xff, 0xd8}},
	{"image/webp", []int{
		0x52, 0x49, 0x46, 0x46, // "RIFF"
		byteWildcard, byteWildcard, byteWildcard, byteWildcard, // file size (variable)
		0x57, 0x45, 0x42, 0x50, // "WEBP"
	}},
	{"image/bmp", []int{0x42, 0x4d, byteWildcard, byteWildcard, byteWildcard, byteWildcard, 0x00, 0x00, 0x00, 0x00}},
	{"image/tiff", []int{0x49, 0x49, 0x2a, 0x00}},
	{"image/tiff", []int{0x4d, 0x4d, 0x00, 0x2a}},
	{"image/avif", []int{
		0x00, 0x00, 0x00, byteWildcard, // box size (variable)
		0x66, 0x74, 0x79, 0x70, 0x61, 0x76, 0x69, 0x66, // "ftypavif"
	}},
	{"image/heic", []int{
		0x00, 0x00, 0x00, byteWildcard, // box size (variable)
		0x66, 0x74, 0x79, 0x70, 0x68, 0x65, 0x69, 0x63, // "ftypheic"
	}},
}

var documentMediaTypeSignatures = []mediaTypeSignature{
	{"application/pdf", []int{0x25, 0x50, 0x44, 0x46}}, // %PDF
}

var audioMediaTypeSignaturesWithoutMp4 = []mediaTypeSignature{
	{"audio/aac", []int{0xff, 0xf0}}, // MPEG-4 ADTS with CRC
	{"audio/aac", []int{0xff, 0xf1}}, // MPEG-4 ADTS without CRC
	{"audio/aac", []int{0xff, 0xf8}}, // MPEG-2 ADTS with CRC
	{"audio/aac", []int{0xff, 0xf9}}, // MPEG-2 ADTS without CRC
	{"audio/mpeg", []int{0xff, 0xfb}},
	{"audio/mpeg", []int{0xff, 0xfa}},
	{"audio/mpeg", []int{0xff, 0xf3}},
	{"audio/mpeg", []int{0xff, 0xf2}},
	{"audio/mpeg", []int{0xff, 0xe3}},
	{"audio/mpeg", []int{0xff, 0xe2}},
	{"audio/wav", []int{
		0x52, 0x49, 0x46, 0x46, // "RIFF"
		byteWildcard, byteWildcard, byteWildcard, byteWildcard,
		0x57, 0x41, 0x56, 0x45, // "WAVE"
	}},
	{"audio/ogg", []int{0x4f, 0x67, 0x67, 0x53}},
	{"audio/flac", []int{0x66, 0x4c, 0x61, 0x43}},
	{"audio/aac", []int{0x40, 0x15, 0x00, 0x00}},
	{"audio/webm", []int{0x1a, 0x45, 0xdf, 0xa3}},
}

// audioMp4Signature covers MP4-container audio (e.g. M4A). It shares the
// ftyp box shape with video/mp4 (see mp4SignatureBytes below); it is included
// only in the audio-specific table, matching TS's split between
// audioMediaTypeSignaturesWithoutMp4 (used for the generic, no-topLevelType
// scan, where an MP4 container can't be told apart from video) and
// audioMediaTypeSignatures (used when the caller already knows to expect
// audio, e.g. transcription).
var audioMp4Signature = mediaTypeSignature{"audio/mp4", mp4SignatureBytes()}

func audioMediaTypeSignatures() []mediaTypeSignature {
	sigs := make([]mediaTypeSignature, 0, len(audioMediaTypeSignaturesWithoutMp4)+1)
	sigs = append(sigs, audioMediaTypeSignaturesWithoutMp4...)
	sigs = append(sigs, audioMp4Signature)
	return sigs
}

func mp4SignatureBytes() []int {
	return []int{0x00, 0x00, 0x00, byteWildcard, 0x66, 0x74, 0x79, 0x70} // ftyp
}

var videoMediaTypeSignatures = []mediaTypeSignature{
	{"video/mp4", mp4SignatureBytes()},
	{"video/webm", []int{0x1a, 0x45, 0xdf, 0xa3}},                                          // EBML
	{"video/quicktime", []int{0x00, 0x00, 0x00, 0x14, 0x66, 0x74, 0x79, 0x70, 0x71, 0x74}}, // ftypqt
	{"video/x-msvideo", []int{0x52, 0x49, 0x46, 0x46}},                                     // RIFF (AVI)
}

const (
	defaultSniffBytes = 18
	// maxSignatureBytes is the longest signature prefix above (image/avif = 12 bytes).
	maxSignatureBytes = 12
)

// MaxID3TagBytes is the largest ID3v2 tag (10-byte header + body) skipped to
// reach the audio frame when sniffing MP3s. It bounds the decode to keep
// detection O(1) in the attachment size. Exported for boundary tests.
const MaxID3TagBytes = 128 * 1024

// id3ScanBytes is the total prefix decoded when an ID3 tag is present: the
// tag plus room for the trailing signature, so a tag right at the size limit
// stays detectable.
const id3ScanBytes = MaxID3TagBytes + maxSignatureBytes

func decodePrefix(data []byte, maxBytes int) []byte {
	if len(data) > maxBytes {
		return data[:maxBytes]
	}
	return data
}

func hasID3(b []byte) bool {
	return len(b) > 10 && b[0] == 0x49 && b[1] == 0x44 && b[2] == 0x33 // "ID3"
}

func stripID3(b []byte) []byte {
	size := (int(b[6]&0x7f) << 21) | (int(b[7]&0x7f) << 14) | (int(b[8]&0x7f) << 7) | int(b[9]&0x7f)
	offset := size + 10
	if offset >= len(b) {
		return nil
	}
	return b[offset:]
}

func detectMediaTypeBySignatures(data []byte, signatures []mediaTypeSignature) (string, bool) {
	b := decodePrefix(data, defaultSniffBytes)

	// ID3v2-tagged MP3s carry the audio frame after the tag; scan a bounded
	// prefix past it rather than decoding the whole input.
	if hasID3(b) {
		b = stripID3(decodePrefix(data, id3ScanBytes))
	}

	for _, sig := range signatures {
		if len(b) < len(sig.bytesPrefix) {
			continue
		}
		match := true
		for i, want := range sig.bytesPrefix {
			if want == byteWildcard {
				continue
			}
			if int(b[i]) != want {
				match = false
				break
			}
		}
		if match {
			return sig.mediaType, true
		}
	}
	return "", false
}

var topLevelSignatureTables = map[string]func() []mediaTypeSignature{
	"image":       func() []mediaTypeSignature { return imageMediaTypeSignatures },
	"audio":       audioMediaTypeSignatures,
	"video":       func() []mediaTypeSignature { return videoMediaTypeSignatures },
	"application": func() []mediaTypeSignature { return documentMediaTypeSignatures },
}

// DetectMediaTypeSignature detects the IANA media type of a file from its raw
// bytes using signature (magic-byte) matching, mirroring the TypeScript SDK's
// detectMediaType.
//
//   - When topLevelType is "", every known signature is considered (image,
//     application, audio, and video). An MP4 container is reported as
//     video/mp4 in this mode, since it can't be distinguished as audio or
//     video by its ftyp box alone.
//   - When topLevelType is "image", "audio", "video", or "application", only
//     signatures for that segment are considered (audio detection then
//     includes the audio/mp4 signature, e.g. for M4A files).
//
// The second return value is false when topLevelType is unrecognized or no
// signature matches.
func DetectMediaTypeSignature(data []byte, topLevelType string) (string, bool) {
	if topLevelType == "" {
		all := make([]mediaTypeSignature, 0,
			len(imageMediaTypeSignatures)+len(documentMediaTypeSignatures)+len(audioMediaTypeSignaturesWithoutMp4)+len(videoMediaTypeSignatures))
		all = append(all, imageMediaTypeSignatures...)
		all = append(all, documentMediaTypeSignatures...)
		all = append(all, audioMediaTypeSignaturesWithoutMp4...)
		all = append(all, videoMediaTypeSignatures...)
		return detectMediaTypeBySignatures(data, all)
	}

	build, ok := topLevelSignatureTables[topLevelType]
	if !ok {
		return "", false
	}
	return detectMediaTypeBySignatures(data, build())
}
