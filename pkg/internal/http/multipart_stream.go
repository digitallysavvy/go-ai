package http

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
)

// MultipartStreamPart is one part of a streaming multipart/form-data upload
// built by NewMultipartStreamBody. Mirrors TypeScript's MultipartStreamPart
// union ({ type: 'field', name, value } | { type: 'file', name, filename,
// mediaType, content }).
type MultipartStreamPart struct {
	// IsFile discriminates a file part (streamed from Content) from a plain
	// field part (Value written as a string).
	IsFile bool

	// Name is the form field name.
	Name string

	// Value is the field value. Only used when IsFile is false.
	Value string

	// Filename is the file part's reported filename. Only used when IsFile
	// is true.
	Filename string

	// MediaType is the file part's Content-Type. Only used when IsFile is
	// true. Defaults to "application/octet-stream" when empty.
	MediaType string

	// Content is the file part's data, read and copied directly into the
	// multipart body without buffering the whole file in memory. Only used
	// when IsFile is true.
	Content io.Reader
}

// NewMultipartStreamBody builds a streaming multipart/form-data request body
// from parts, writing them in order (mirrors TypeScript's
// postMultipartStreamToApi: fields are written before the file part so a
// streaming server can read the purpose/metadata fields before consuming the
// potentially large file part). Only the file part's Content is streamed
// lazily via io.Pipe; field values are small and written eagerly.
//
// Returns the request body reader (must be closed by the caller after use;
// closing before it's been fully read cancels the in-flight write goroutine
// and propagates ErrClosedPipe to any pending Content read) and the full
// Content-Type header value, including the boundary.
func NewMultipartStreamBody(parts []MultipartStreamPart) (io.ReadCloser, string, error) {
	boundary, err := randomMultipartBoundary()
	if err != nil {
		return nil, "", err
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	if err := mw.SetBoundary(boundary); err != nil {
		return nil, "", err
	}

	go func() {
		var writeErr error
		for _, part := range parts {
			if !part.IsFile {
				if writeErr = mw.WriteField(part.Name, part.Value); writeErr != nil {
					break
				}
				continue
			}

			mediaType := part.MediaType
			if mediaType == "" {
				mediaType = "application/octet-stream"
			}
			var fw io.Writer
			fw, writeErr = createFormFilePart(mw, part.Name, part.Filename, mediaType)
			if writeErr != nil {
				break
			}
			if part.Content != nil {
				if _, writeErr = io.Copy(fw, part.Content); writeErr != nil {
					break
				}
			}
		}
		if writeErr == nil {
			writeErr = mw.Close()
		}
		// pw.CloseWithError(nil) is equivalent to pw.Close(): io.Pipe reports a
		// nil error as io.EOF to the reader side.
		_ = pw.CloseWithError(writeErr)
	}()

	return pr, mw.FormDataContentType(), nil
}

// createFormFilePart is like multipart.Writer.CreateFormFile, but accepts an
// explicit media type instead of hardcoding "application/octet-stream".
func createFormFilePart(w *multipart.Writer, fieldName, filename, mediaType string) (io.Writer, error) {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
		escapeMultipartQuotes(fieldName), escapeMultipartQuotes(filename)))
	h.Set("Content-Type", mediaType)
	return w.CreatePart(h)
}

func escapeMultipartQuotes(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return replacer.Replace(s)
}

// randomMultipartBoundary returns a random boundary prefixed
// "ai-sdk-multipart-", matching the TS SDK's postMultipartStreamToApi
// boundary naming for easier cross-SDK debugging.
func randomMultipartBoundary() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "ai-sdk-multipart-" + hex.EncodeToString(buf), nil
}
