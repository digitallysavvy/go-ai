package prompt

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r84integJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TS convert-to-google-messages.ts 339 / 127-150 (tests 458 and 1061): a gs:
// URL is sent as data.originalUrl when the caller's string differs from its
// serialization; any other scheme sends url.toString().
func TestR84integGoogleKeepsOriginalGCSURL(t *testing.T) {
	user := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{MediaType: "application/pdf", FileData: types.FileData{
			Type: types.FileDataTypeURL, URL: "gs://my-bucket/folder/My%20File.pdf", OriginalURL: "gs://my-bucket/folder/My File.pdf"}},
		types.FileContent{MediaType: "application/pdf", FileData: types.FileData{
			Type: types.FileDataTypeURL, URL: "https://x.test/a%20b.pdf", OriginalURL: "HTTPS://X.test/a b.pdf"}},
	}}}
	out, err := ConvertToGoogleMessages(user, GoogleMessagesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := r84integJSON(t, out.Contents)
	for _, want := range []string{`"fileUri":"gs://my-bucket/folder/My File.pdf"`, `"fileUri":"https://x.test/a%20b.pdf"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("contents = %s, want %s", got, want)
		}
	}

	tool := []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
		ToolCallID: "testCallId", ToolName: "imageGenerator",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
			types.FileContentBlock{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL,
				URL: "gs://example-bucket/renditions/My%20Hero.png", OriginalURL: "gs://example-bucket/renditions/My Hero.png"}},
		}},
	}}}}
	out, err = ConvertToGoogleMessages(tool, GoogleMessagesOptions{IncludeFunctionCallIDs: true, SupportsFunctionResponseParts: true,
		// Matches only the original string: TS isUrlSupported runs on supportedUrl.
		SupportedFunctionResponseURLs: map[string][]string{"*": {`^gs://.* .*$`}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := r84integJSON(t, out.Contents); !strings.Contains(got, `"fileData":{"fileUri":"gs://example-bucket/renditions/My Hero.png","mimeType":"image/png"}`) {
		t.Fatalf("tool contents = %s", got)
	}
}

// TS resolveFullMediaType: a user URL file part with only a top-level media
// type fails (it is not inline bytes); r84 decision, the v0.5.2 completion
// from the URL extension was neither documented nor exported.
func TestR84integGoogleURLPartNeedsSubtype(t *testing.T) {
	for _, mt := range []string{"image", "image/*"} {
		_, err := ConvertToGoogleMessages([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{MediaType: mt, FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://x.test/a.png"}},
		}}}, GoogleMessagesOptions{})
		var unsupported *providererrors.UnsupportedFunctionalityError
		if !errors.As(err, &unsupported) || unsupported.Functionality != `file of media type "`+mt+`" must specify subtype since it is not passed as inline bytes` {
			t.Fatalf("%s: err = %v", mt, err)
		}
	}
	out, err := ConvertToGoogleMessages([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://x.test/a"}},
	}}}, GoogleMessagesOptions{})
	if err != nil || !strings.Contains(r84integJSON(t, out.Contents), `"mimeType":"image/png"`) {
		t.Fatalf("full media type: %v", err)
	}
}
