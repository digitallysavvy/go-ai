package http

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewMultipartStreamBody_FieldsPrecedeFilePart(t *testing.T) {
	content := strings.NewReader(`{"a":1}` + "\n" + `{"b":2}` + "\n")
	body, contentType, err := NewMultipartStreamBody([]MultipartStreamPart{
		{Name: "purpose", Value: "batch"},
		{Name: "expires_after[anchor]", Value: "created_at"},
		{Name: "expires_after[seconds]", Value: "172800"},
		{IsFile: true, Name: "file", Filename: "batch.jsonl", MediaType: "application/jsonl", Content: content},
	})
	if err != nil {
		t.Fatalf("NewMultipartStreamBody: %v", err)
	}
	defer body.Close()

	if !strings.HasPrefix(contentType, "multipart/form-data; boundary=ai-sdk-multipart-") {
		t.Fatalf("Content-Type = %q", contentType)
	}

	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("ParseMediaType: %v", err)
	}
	mr := multipart.NewReader(body, params["boundary"])

	var order []string
	form := map[string]string{}
	var fileContent []byte
	var fileMediaType, fileName string

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		name := part.FormName()
		order = append(order, name)
		if part.FileName() != "" {
			fileName = part.FileName()
			fileMediaType = part.Header.Get("Content-Type")
			fileContent, err = io.ReadAll(part)
			if err != nil {
				t.Fatalf("read file part: %v", err)
			}
			continue
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read field part: %v", err)
		}
		form[name] = string(data)
	}

	wantOrder := []string{"purpose", "expires_after[anchor]", "expires_after[seconds]", "file"}
	if len(order) != len(wantOrder) {
		t.Fatalf("part order = %v, want %v", order, wantOrder)
	}
	for i, name := range wantOrder {
		if order[i] != name {
			t.Fatalf("part order = %v, want %v", order, wantOrder)
		}
	}

	if form["purpose"] != "batch" {
		t.Fatalf("purpose = %q", form["purpose"])
	}
	if form["expires_after[anchor]"] != "created_at" || form["expires_after[seconds]"] != "172800" {
		t.Fatalf("expires_after fields = %+v", form)
	}
	if fileName != "batch.jsonl" {
		t.Fatalf("filename = %q", fileName)
	}
	if fileMediaType != "application/jsonl" {
		t.Fatalf("file Content-Type = %q", fileMediaType)
	}
	if string(fileContent) != "{\"a\":1}\n{\"b\":2}\n" {
		t.Fatalf("file content = %q", string(fileContent))
	}
}

func TestNewMultipartStreamBody_DefaultsMediaType(t *testing.T) {
	body, contentType, err := NewMultipartStreamBody([]MultipartStreamPart{
		{IsFile: true, Name: "file", Filename: "blob", Content: strings.NewReader("x")},
	})
	if err != nil {
		t.Fatalf("NewMultipartStreamBody: %v", err)
	}
	defer body.Close()

	_, params, _ := mime.ParseMediaType(contentType)
	mr := multipart.NewReader(body, params["boundary"])
	part, err := mr.NextPart()
	if err != nil {
		t.Fatalf("NextPart: %v", err)
	}
	if got := part.Header.Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", got)
	}
}

func TestNewMultipartStreamBody_PropagatesContentReadError(t *testing.T) {
	body, contentType, err := NewMultipartStreamBody([]MultipartStreamPart{
		{IsFile: true, Name: "file", Filename: "f", Content: errReader{}},
	})
	if err != nil {
		t.Fatalf("NewMultipartStreamBody: %v", err)
	}
	defer body.Close()

	_, params, _ := mime.ParseMediaType(contentType)
	mr := multipart.NewReader(body, params["boundary"])
	part, err := mr.NextPart()
	if err != nil {
		t.Fatalf("NextPart: %v", err)
	}
	if _, err := io.ReadAll(part); err == nil {
		t.Fatal("expected the content read error to propagate through the pipe")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrClosedPipe }

// TestNewMultipartStreamBody_CloseWithoutReadingDoesNotLeakGoroutine verifies
// that abandoning the returned body (mirroring a cancelled or failed HTTP
// request, which the standard library's Transport always closes even when
// it never fully reads the body) unblocks the internal writer goroutine
// instead of leaving it parked forever on a pipe write nobody will ever
// read.
func TestNewMultipartStreamBody_CloseWithoutReadingDoesNotLeakGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	body, _, err := NewMultipartStreamBody([]MultipartStreamPart{
		{Name: "purpose", Value: "batch"},
		{IsFile: true, Name: "file", Filename: "f", Content: bytes.NewReader(make([]byte, 1<<20))},
	})
	if err != nil {
		t.Fatalf("NewMultipartStreamBody: %v", err)
	}

	// Close immediately, before anything is read from the pipe. The writer
	// goroutine is necessarily blocked (or about to block) on its first
	// pipe write at this point, since io.Pipe is unbuffered and nothing has
	// consumed it yet.
	if err := body.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if runtime.NumGoroutine() <= before {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leak: NumGoroutine before=%d, still elevated after=%d", before, runtime.NumGoroutine())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
