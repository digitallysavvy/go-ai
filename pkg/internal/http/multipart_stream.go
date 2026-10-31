package http

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"strings"
	"sync"
	"sync/atomic"
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
	body := &multipartBody{pr: pr, parts: parts}

	go func() {
		writeErr := writeMultipartParts(pw, boundary, parts)
		// pw.CloseWithError(nil) is equivalent to pw.Close(): io.Pipe reports a
		// nil error as io.EOF to the reader side.
		if writeErr == nil {
			body.completed.Store(true)
		}
		_ = pw.CloseWithError(writeErr)
		if writeErr != nil {
			// TS dispose(): release every source part after a failed write.
			body.releaseContents()
		}
	}()

	return body, "multipart/form-data; boundary=" + boundary, nil
}

// multipartBody is the read side of the pipe. Close tears the upload down the
// way TS createMultipartBody.dispose does: the pipe is closed and every
// closable file Content is released (a blocked Read is interrupted).
type multipartBody struct {
	pr        *io.PipeReader
	parts     []MultipartStreamPart
	once      sync.Once
	completed atomic.Bool // every part was fully written
}

func (b *multipartBody) Read(p []byte) (int, error) { return b.pr.Read(p) }

func (b *multipartBody) Close() error {
	err := b.pr.Close()
	b.releaseContents()
	return err
}

func (b *multipartBody) releaseContents() {
	b.once.Do(func() {
		if b.completed.Load() {
			return
		}
		for _, part := range b.parts {
			if !part.IsFile || part.Content == nil {
				continue
			}
			if c, ok := part.Content.(io.Closer); ok {
				_ = c.Close()
			}
		}
	})
}

// multipartHeaderValue mirrors TS escapeMultipartHeaderValue: CR/LF are
// dropped (no header smuggling) and backslash and quote are escaped.
func multipartHeaderValue(value string) string {
	value = strings.NewReplacer("\r", "", "\n", "").Replace(value)
	return escapeMultipartQuotes(value)
}

func writeMultipartParts(w io.Writer, boundary string, parts []MultipartStreamPart) error {
	for _, part := range parts {
		disposition := "--" + boundary + "\r\nContent-Disposition: form-data; name=\"" + multipartHeaderValue(part.Name) + "\""
		if !part.IsFile {
			if _, err := io.WriteString(w, disposition+"\r\n\r\n"+part.Value+"\r\n"); err != nil {
				return err
			}
			continue
		}

		// A filename parameter is always emitted (TS defaults it to "blob"):
		// without it multipart parsers treat the part as a scalar field.
		filename := part.Filename
		if filename == "" {
			filename = "blob"
		}
		mediaType := part.MediaType
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		mediaType = strings.NewReplacer("\r", "", "\n", "").Replace(mediaType)
		header := disposition + "; filename=\"" + multipartHeaderValue(filename) + "\"\r\nContent-Type: " + mediaType + "\r\n\r\n"
		if _, err := io.WriteString(w, header); err != nil {
			return err
		}
		if part.Content != nil {
			if _, err := io.Copy(w, part.Content); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, "\r\n"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "--"+boundary+"--\r\n")
	return err
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
