package topaz

import (
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS names the uploaded image part `image.<mediaTypeToExtension(mediaType)>`
// (the lower-cased subtype), so image/jpeg is image.jpeg and image/webp is
// image.webp.
func TestR35_ImageUploadFilenameFollowsMediaTypeToExtension(t *testing.T) {
	for mediaType, want := range map[string]string{
		"image/jpeg": "image.jpeg",
		"image/webp": "image.webp",
		"image/tiff": "image.tiff",
		"image/png":  "image.png",
	} {
		m := NewImageModel(New(Config{APIKey: "k"}), ImageModelWonder35)
		opts := &provider.ImageGenerateOptions{Files: []provider.ImageFile{{Type: "file", MediaType: mediaType, Data: []byte{1}}}}
		var w []types.Warning
		body, contentType, err := m.buildFormData(opts, nil, &w)
		if err != nil {
			t.Fatal(err)
		}
		_, params, _ := mime.ParseMediaType(contentType)
		mr := multipart.NewReader(body, params["boundary"])
		var got string
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if part.FormName() == "image" {
				got = part.FileName()
			}
		}
		if got != want {
			t.Errorf("%s: filename = %q, want %q", mediaType, got, want)
		}
	}
}

// TS names the status of the current poll in the timeout message
// (`status.status ?? 'unknown'`).
func TestR35_TimeoutMessageNamesCurrentPollStatus(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()
	ts.statusFn = func(count int) (int, string) {
		if count == 1 {
			return 200, `{"status":"Processing"}`
		}
		return 200, `{}`
	}
	opts := defaultImageOptions(ts)
	opts.ProviderOptions = map[string]interface{}{
		"topaz": map[string]interface{}{"pollIntervalMillis": 30, "pollTimeoutMillis": 50},
	}
	_, err := ts.model().DoGenerate(t.Context(), opts)
	if err == nil || !strings.Contains(err.Error(), "last status unknown") {
		t.Fatalf("error = %v, want 'last status unknown'", err)
	}
}

// TS containerFromUrl is `split('.').pop()`: a URL with no dot is its own
// "extension".
func TestR35_ContainerFromURLWithoutDot(t *testing.T) {
	if got := containerFromURL("mp4"); got != "mp4" {
		t.Errorf("containerFromURL(mp4) = %q, want mp4", got)
	}
	if got := containerFromURL("https://x.test/clip"); got != "" {
		t.Errorf("no-extension URL = %q, want empty", got)
	}
}
