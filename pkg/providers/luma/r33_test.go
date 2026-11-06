package luma

import (
	"net/http"
	"strings"
	"testing"

	"errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// JS enumerates integer-like keys first (ascending), so identity "2" is
// validated before "zeta" even though it was inserted later.
func TestR33_IdentityValidationFollowsJSKeyOrder(t *testing.T) {
	model := NewImageModel(New(Config{APIKey: "k"}), ModelPhoton1)
	rt := ReferenceTypeCharacter
	var files []provider.ImageFile
	var configs []ImageConfig
	for _, id := range []string{"zeta", "2"} {
		id := id
		for i := 0; i < 5; i++ {
			files = append(files, provider.ImageFile{Type: "url", URL: "https://x/" + id})
			configs = append(configs, ImageConfig{ID: &id})
		}
	}
	_, err := model.getEditingOptions(files, nil, &ImageModelOptions{ReferenceType: &rt, Images: configs})
	if err == nil || !strings.Contains(err.Error(), "Identity '2' has 5 images.") {
		t.Fatalf("err = %v", err)
	}
}

func TestR33_ErrorKeepsResponseHeaders(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k"}), ModelPhoton1)
	err := m.parseErrorResponseWithHeaders([]byte(`oops`), 500, http.Header{"X-Request-Id": {"r1"}})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.ResponseHeaders["X-Request-Id"] != "r1" {
		t.Fatalf("got %#v", perr)
	}
}
