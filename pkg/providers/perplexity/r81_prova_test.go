package perplexity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A 2xx embeddings response with `data: []` indexed [0] unchecked in DoEmbed
// and panicked. TS embed() rejects it with InvalidResponseDataError
// "No embedding generated.".
func TestR81provaPerplexityDoEmbedEmptyDataErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[],"model":"m","usage":{"prompt_tokens":1}}`))
	}))
	defer srv.Close()
	m := NewEmbeddingModel(New(Config{BaseURL: srv.URL, APIKey: "k"}), "pplx-embed-v1-4b")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DoEmbed panicked: %v", r)
		}
	}()
	_, err := m.DoEmbed(context.Background(), "hi", nil)
	var invalid *providererrors.InvalidResponseDataError
	if !errors.As(err, &invalid) || invalid.Message != "No embedding generated." {
		t.Fatalf("err = %T %v, want InvalidResponseDataError \"No embedding generated.\"", err, err)
	}
}

// TS detectMediaType sniffs a bare/wildcard image type from the base64
// string with convertBase64ToUint8Array, so unpadded data is detected; Go
// decoded with base64.StdEncoding and rejected it.
func TestR81provaPerplexityDecisionSniffsUnpaddedBase64(t *testing.T) {
	got, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{imageState("image/*", "iVBORw")})
	want := map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,iVBORw"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, %v", got, err)
	}
}

// TS input_image embeds convertToBase64(part.data.data), which returns a
// base64 string payload as is; Go read only the bytes, so a DataString image
// became an empty data URL.
func TestR81provaPerplexityImageDataStringPayload(t *testing.T) {
	for _, tc := range []struct{ mediaType, data, want string }{
		{"image/png", "-_8", "data:image/png;base64,-_8"},
		{"image/*", "iVBORw", "data:image/png;base64,iVBORw"},
	} {
		got, err := convertPerplexityUserPart(types.FileContent{MediaType: tc.mediaType, FileData: types.FileData{Type: types.FileDataTypeData, DataString: tc.data}})
		if err != nil || got["image_url"] != tc.want {
			t.Fatalf("%s: part = %v, %v, want image_url %s", tc.mediaType, got, err, tc.want)
		}
	}
}

// TS isFullMediaType: "image/" (empty subtype) is not a full media type, so
// the type is sniffed from the data; Go only sniffed a type without "/" or
// ending in "/*" and rejected "image/".
func TestR81provaPerplexityDecisionEmptySubtypeIsSniffed(t *testing.T) {
	got, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{imageState("image/", "iVBORw==")})
	if err != nil || !reflect.DeepEqual(got, pngImage) {
		t.Fatalf("got %#v, %v", got, err)
	}
}
