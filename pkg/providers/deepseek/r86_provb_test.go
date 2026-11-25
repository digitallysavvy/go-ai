package deepseek

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR86provbToolCallArgumentsMatchNode: replayed tool-call argument text
// against TS 30548ee7f9 outputs computed under Node from a literal copy of
// the TS converter's serializer (serialize; the TS input is the parsed JSON, or
// the raw string for an invalid call).
func TestR86provbToolCallArgumentsMatchNode(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"a":1}`, `{"a":1}`},
		{`[1,2]`, `[1,2]`},
		{`"{\"a\":1}"`, `{"a":1}`},
		{`"[1]"`, `[1]`},
		{`"abc"`, `"abc"`},
		{`abc`, `"abc"`},
		{`{"a":`, `"{\"a\":"`},
		{`123`, `123`},
		{`"\"x\""`, `"x"`},
		{`{"b":"<&>"}`, `{"b":"<&>"}`},
	} {
		if got := deepseekToolCallsWire([]types.ToolCall{{ID: "c", ToolName: "f", RawArguments: tc.raw}})[0]["function"].(map[string]interface{})["arguments"]; got != tc.want {
			t.Errorf("%s: arguments = %s, want %s", tc.raw, got, tc.want)
		}
	}
}

// TestR86provbFilesOptionsParsedFirst: TS parses the provider options before
// it converts and validates the file, so a call with both an invalid
// expiresAfter and an unsupported file reports the options error
// (InvalidArgumentError "invalid deepseek provider options"); a null
// expiresAfter is rejected too (z.number().optional()).
func TestR86provbFilesOptionsParsedFirst(t *testing.T) {
	files := &FilesAPI{provider: New(Config{APIKey: "k"})}
	for _, v := range []interface{}{60, nil, "3600"} {
		_, err := files.UploadFile(context.Background(), types.UploadFileOptions{
			Data:            types.FileData{Type: types.FileDataTypeData, Data: []byte("%PDF-1.4")},
			MediaType:       "application/pdf",
			ProviderOptions: map[string]interface{}{"deepseek": map[string]interface{}{"expiresAfter": v}},
		})
		var invalid *providererrors.InvalidArgumentError
		if !errors.As(err, &invalid) || invalid.Field != "providerOptions" || invalid.Message != "invalid deepseek provider options" {
			t.Fatalf("expiresAfter %v: err = %v", v, err)
		}
	}
}

// TestR86provbFilesEmptyResponseFilename: TS `response.filename ?? filename`
// keeps a returned "": the metadata carries filename "", and the result has
// no filename (not the request's).
func TestR86provbFilesEmptyResponseFilename(t *testing.T) {
	p := newDeepseekProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"id":"file-1","filename":""}`))}, nil
	})
	result, err := (&FilesAPI{provider: p}).UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{0x89, 0x50, 0x4e, 0x47}},
		MediaType: "image/png",
		Filename:  "request.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Filename != "" {
		t.Fatalf("Filename = %q, want none", result.Filename)
	}
	meta := result.ProviderMetadata["deepseek"].(map[string]interface{})
	if v, ok := meta["filename"]; !ok || v != "" {
		t.Fatalf("metadata = %v, want filename \"\"", meta)
	}
}
