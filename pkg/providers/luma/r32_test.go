package luma

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS validates identities in insertion order, so when several exceed four
// images the first inserted one is the one reported.
func TestCharacterIdentityLimitReportsFirstIdentity(t *testing.T) {
	model := NewImageModel(New(Config{APIKey: "k"}), ModelPhoton1)
	rt := ReferenceTypeCharacter
	var files []provider.ImageFile
	var configs []ImageConfig
	for _, id := range []string{"zeta", "alpha", "mid"} {
		id := id
		for i := 0; i < 5; i++ {
			files = append(files, provider.ImageFile{Type: "url", URL: "https://x/" + id})
			configs = append(configs, ImageConfig{ID: &id})
		}
	}
	for run := 0; run < 30; run++ {
		_, err := model.getEditingOptions(files, nil, &ImageModelOptions{ReferenceType: &rt, Images: configs})
		if err == nil || !strings.Contains(err.Error(), "Identity 'zeta' has 5 images.") {
			t.Fatalf("run %d: err = %v", run, err)
		}
	}
}
