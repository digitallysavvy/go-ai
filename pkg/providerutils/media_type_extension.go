package providerutils

import (
	"fmt"
	"mime/multipart"
	"net/textproto"
	"strings"
)

// MediaTypeToExtension maps a media type to a file extension, mirroring TS
// provider-utils mediaTypeToExtension: the lower-cased subtype, with mpeg ->
// mp3, x-wav -> wav, opus -> ogg and mp4/x-m4a -> m4a. It is used to name
// audio uploads (some APIs infer the container from the filename).
func MediaTypeToExtension(mediaType string) string {
	subtype := ""
	if parts := strings.SplitN(strings.ToLower(mediaType), "/", 2); len(parts) == 2 {
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
// stdlib helper always sends application/octet-stream). An empty mediaType
// falls back to application/octet-stream.
func CreateTypedFormFile(w *multipart.Writer, field, filename, mediaType string) (interface {
	Write([]byte) (int, error)
}, error) {
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filename))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	header.Set("Content-Type", mediaType)
	return w.CreatePart(header)
}
