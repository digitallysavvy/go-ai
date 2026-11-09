package providerutils

import (
	"mime/multipart"
	"strings"
)

// MediaTypeToExtension maps a media type to a file extension, mirroring TS
// provider-utils mediaTypeToExtension: the lower-cased subtype, with mpeg ->
// mp3, x-wav -> wav, opus -> ogg and mp4/x-m4a -> m4a. It is used to name
// audio uploads (some APIs infer the container from the filename).
func MediaTypeToExtension(mediaType string) string {
	// TS: const [_type, subtype = ''] = mediaType.toLowerCase().split('/'),
	// the second segment only ("a/b/c" has subtype "b").
	subtype := ""
	if parts := strings.SplitN(JSToLower(mediaType), "/", 3); len(parts) >= 2 {
		subtype = parts[1]
	}
	switch subtype {
	case "mpeg":
		return "mp3"
	case "x-wav":
		return "wav"
	case "opus":
		return "ogg"
	case "mp4", "x-m4a":
		return "m4a"
	default:
		return subtype
	}
}

// CreateTypedFormFile is multipart.Writer.CreateFormFile with an explicit
// part Content-Type (TS uploads a File whose type is the media type; the
// stdlib helper always sends application/octet-stream). The part header
// follows TS FormData (see FormDataFilePartHeader): the media type is
// lowercased like a Blob type, an empty one is application/octet-stream,
// and the name and filename escape `"`, CR and LF as %22, %0D and %0A.
func CreateTypedFormFile(w *multipart.Writer, field, filename, mediaType string) (interface {
	Write([]byte) (int, error)
}, error) {
	return CreateFormDataFile(w, field, filename, mediaType)
}
