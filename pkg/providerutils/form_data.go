package providerutils

import (
	"mime/multipart"
	"net/textproto"
	"strings"
)

// FormDataBlobType is the type of a JS Blob or File created with
// {type: mediaType}: the Blob constructor ASCII-lowercases it, and uses ""
// when it holds a character outside U+0020-U+007E (File API, "type").
func FormDataBlobType(mediaType string) string {
	for i := 0; i < len(mediaType); i++ {
		if c := mediaType[i]; c < 0x20 || c > 0x7e {
			return ""
		}
	}
	return strings.ToLower(mediaType)
}

// formDataEscaper is undici's formdataEscape for multipart parameter values.
var formDataEscaper = strings.NewReplacer("\n", "%0A", "\r", "%0D", `"`, "%22")

// formDataLinefeeds is undici's normalizeLinefeeds: every CR, LF or CRLF
// becomes CRLF.
func formDataLinefeeds(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// FormDataFilePartHeader is the part header TS fetch (undici) writes for a
// Blob or File appended to a FormData: `form-data; name="<field>"` with the
// field's line breaks normalized to CRLF and then escaped (`"` as %22, LF
// as %0A, CR as %0D, backslash kept), `; filename="<filename>"` escaped the
// same way and left out when filename is empty (a File named ""), and the
// Content-Type of the Blob type (see FormDataBlobType), or
// application/octet-stream when that is empty.
func FormDataFilePartHeader(field, filename, mediaType string) textproto.MIMEHeader {
	disposition := `form-data; name="` + formDataEscaper.Replace(formDataLinefeeds(field)) + `"`
	if filename != "" {
		disposition += `; filename="` + formDataEscaper.Replace(filename) + `"`
	}
	contentType := FormDataBlobType(mediaType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", disposition)
	header.Set("Content-Type", contentType)
	return header
}

// CreateFormDataFile creates a file part the way TS FormData.append(field,
// new Blob([...], {type: mediaType}), filename) serializes it (see
// FormDataFilePartHeader).
func CreateFormDataFile(w *multipart.Writer, field, filename, mediaType string) (interface {
	Write([]byte) (int, error)
}, error) {
	return w.CreatePart(FormDataFilePartHeader(field, filename, mediaType))
}

// WriteFormDataField writes a string field the way TS FormData.append(field,
// value) serializes it (undici): line breaks in the value and in the name
// become CRLF, and the name escapes `"`, CR and LF as %22, %0D and %0A.
func WriteFormDataField(w *multipart.Writer, field, value string) error {
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="`+formDataEscaper.Replace(formDataLinefeeds(field))+`"`)
	part, err := w.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write([]byte(formDataLinefeeds(value)))
	return err
}
