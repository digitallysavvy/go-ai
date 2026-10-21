package prodia

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS parses the multipart job part with prodiaJobResultSchema: `id` is
// required and a present price needs both product and dollars. A mismatch is
// the TypeValidationError itself.
func TestR35_JobPartIsValidatedAgainstTSSchema(t *testing.T) {
	for name, job := range map[string]string{
		"missing id":       `{"config":{"seed":1}}`,
		"price no product": `{"id":"j","price":{"dollars":1}}`,
		"state no current": `{"id":"j","state":{}}`,
		"null created_at":  `{"id":"j","created_at":null}`,
		"non-numeric seed": `{"id":"j","config":{"seed":"x"}}`,
	} {
		body, ct := buildTestMultipartBody(job, []byte("x"), "image/png")
		_, _, _, err := parseMultipartResponseFor(ct, body, "image/")
		var tv *providererrors.TypeValidationError
		if err == nil || !errors.As(err, &tv) {
			t.Errorf("%s: err = %v (%T), want TypeValidationError", name, err, err)
		}
	}
	body, ct := buildTestMultipartBody(`{"id":"j","price":null,"config":{"seed":1,"x":2},"extra":1}`, []byte("x"), "image/png")
	if _, _, _, err := parseMultipartResponseFor(ct, body, "image/"); err != nil {
		t.Errorf("valid job rejected: %v", err)
	}
}
