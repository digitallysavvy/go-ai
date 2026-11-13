package bfl

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonResponseHandler: JSON.parse reads 1e999 as Infinity and zod
// strips the undeclared key (ruling 4).
func TestR98provcBFLBodiesOutOfRangeUndeclared(t *testing.T) {
	for name, check := range map[string]func([]byte) error{
		"image submit": validateBFLImageSubmitBody,
		"video submit": validateBFLSubmitBody,
	} {
		if err := check([]byte(`{"id":"t1","polling_url":"https://api.bfl.ai/v1/get_result?id=t1","queue":1e999}`)); err != nil {
			t.Errorf("%s: want valid, got %v", name, err)
		}
	}
	if err := validateBFLPollBody([]byte(`{"status":"Pending","progress":1e999}`)); err != nil {
		t.Errorf("image poll: want valid, got %v", err)
	}
	if err := validateBFLVideoPollBody([]byte(`{"status":"Pending","progress":1e999}`)); err != nil {
		t.Errorf("video poll: want valid, got %v", err)
	}
}

// secureJsonParse rejects a __proto__ key before the schema runs: the
// validator fails with the JSONParseError (the caller's "Invalid JSON
// response" cause) instead of passing it to the typed decode.
func TestR98provcBFLBodiesPrototypeKey(t *testing.T) {
	body := []byte(`{"id":"t1","polling_url":"https://api.bfl.ai/v1/get_result?id=t1","__proto__":{"x":1}}`)
	for name, check := range map[string]func([]byte) error{
		"image submit": validateBFLImageSubmitBody,
		"video submit": validateBFLSubmitBody,
		"image poll":   validateBFLPollBody,
		"video poll":   validateBFLVideoPollBody,
	} {
		err := check(body)
		var pe *providererrors.JSONParseError
		if !errors.As(err, &pe) {
			t.Errorf("%s: want JSONParseError, got %v", name, err)
		}
	}
}
