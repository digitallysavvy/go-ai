package googlevertex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TS google-vertex-image-model.test.ts "should reject unsupported URL editing
// input and masks": a URL file is sent as `image/*`, which the Google prompt
// conversion rejects before any request, even when the URL has an image
// extension or the file names a media type.
func TestR65VertexImageRejectsURLFile(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer server.Close()
	prov, err := New(Config{Project: "p", Location: "us-central1", AccessToken: "t", BaseURL: server.URL})
	require.NoError(t, err)
	model := NewImageModel(prov, "gemini-2.5-flash-image")

	for _, file := range []provider.ImageFile{
		{Type: "url", URL: "https://example.com/cat.png"},
		{Type: "url", URL: "gs://bucket/cat.png", MediaType: "image/png"},
	} {
		_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
			Prompt: "Add a hat to this cat",
			Files:  []provider.ImageFile{file},
		})
		var unsupported *providererrors.UnsupportedFunctionalityError
		require.ErrorAs(t, err, &unsupported, file.URL)
		assert.Regexp(t, `media type "image/\*".*not passed as inline bytes`, err.Error())
	}
	assert.False(t, called, "no request may be sent")
}
