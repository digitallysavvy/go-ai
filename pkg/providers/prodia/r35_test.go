package prodia

import (
	"bytes"
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

// TS sends JSON.stringify(body) as the job part: key order kept, no HTML
// escaping, no trailing newline.
func TestR35_JobPartIsPlainJSONStringify(t *testing.T) {
	buf, ct, err := buildMultipartJobRequest(map[string]interface{}{
		"type":   "m",
		"config": map[string]interface{}{"resolution": "720p", "prompt": "a<b&c", "seed": 1},
	}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = ct
	want := `{"type":"m","config":{"prompt":"a<b&c","seed":1,"resolution":"720p"}}`
	if !bytes.Contains(buf.Bytes(), []byte(want+"\r\n--")) {
		t.Fatalf("job part not %s in:\n%s", want, buf.String())
	}
}
