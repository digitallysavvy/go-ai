package luma

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS createJsonResponseHandler parses with JSON.parse, which reads 1e999 as
// Infinity; zod strips the undeclared key, so the body is valid (ruling 4).
func TestR98provcLumaGenerationOutOfRangeUndeclared(t *testing.T) {
	body := []byte(`{"id":"g1","state":"completed","assets":{"image":"https://x/a.png"},"extra":1e999}`)
	if err := validateLumaGeneration(200, nil, body); err != nil {
		t.Fatalf("want valid, got %v", err)
	}
	// A __proto__ key still fails as "Invalid JSON response" (secureJsonParse).
	bad := []byte(`{"id":"g1","state":"completed","__proto__":{"x":1}}`)
	if err := validateLumaGeneration(200, nil, bad); !providerutils.IsInvalidJSONResponse(err) {
		t.Fatalf("want Invalid JSON response, got %v", err)
	}
}
