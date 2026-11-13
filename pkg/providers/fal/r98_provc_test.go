package fal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS createJsonResponseHandler: JSON.parse reads 1e999 as Infinity and zod
// strips the undeclared key (ruling 4).
func TestR98provcFalVideoResponseOutOfRangeUndeclared(t *testing.T) {
	body := []byte(`{"video":{"url":"https://x/v.mp4","bitrate":1e999},"seed":1}`)
	if err := validateVideoStatus(200, nil, body); err != nil {
		t.Fatalf("status: want valid, got %v", err)
	}
	job := []byte(`{"request_id":"r1","queue_position":1e999}`)
	if err := validateVideoJob(200, nil, job); err != nil {
		t.Fatalf("job: want valid, got %v", err)
	}
}

// falImageSchema url is z.string(): an empty url passes the schema and TS
// getFromApi (validateUrl) rejects it with DownloadError "Invalid URL: ".
func TestR98provcFalImageEmptyURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"images":[{"url":""}]}`))
	}))
	defer srv.Close()
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "fal-fast")
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	var dl *providererrors.DownloadError
	if !errors.As(err, &dl) || dl.Message != "Invalid URL: " {
		t.Fatalf("want DownloadError \"Invalid URL: \", got %T %v", err, err)
	}
}

// falImageResponseSchema pipes the union result through
// base().extend({images: z.array(falImageSchema())}): when the `image`
// branch matched, a passthrough `images` key that is not a valid image list
// fails the parse ("Invalid JSON response") instead of being used.
func TestR98provcFalImagePipeRejectsLooseImages(t *testing.T) {
	for _, body := range []string{
		`{"images":[{"no_url":1}],"image":{"url":"https://x/a.png"}}`,
		`{"images":null,"image":{"url":"https://x/a.png"}}`,
	} {
		if err := providerutils.MediaResponseValidator("fal", providerutils.SchemaFalImage)(200, nil, []byte(body)); !providerutils.IsInvalidJSONResponse(err) {
			t.Errorf("%s: want Invalid JSON response, got %v", body, err)
		}
	}
	if err := providerutils.MediaResponseValidator("fal", providerutils.SchemaFalImage)(200, nil, []byte(`{"image":{"url":"https://x/a.png"},"seed":1}`)); err != nil {
		t.Errorf("single image: want valid, got %v", err)
	}
}
