package providerutils

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS `await response.json()` then schema.parse (MediaSchemaParserPlain): plain
// JSON.parse keeps a __proto__ key as an ordinary property, so a body that
// carries one and fails the schema is the TypeValidationError. The secure
// variant (safeParseJSON) reports the JSONParseError.
func TestR98carryChunkParseFailurePlainKeepsPrototypeKeys(t *testing.T) {
	body := `{"__proto__":{"x":1},"id":5}`
	issue := &ChunkSchemaError{Path: []interface{}{"id"}, Code: "invalid_type", Message: "Invalid input: expected string, received number"}

	_, plain := ChunkParseFailurePlain(body, issue)
	var validation *providererrors.ValidationError
	if !errors.As(plain, &validation) {
		t.Fatalf("plain = %T %v, want *ValidationError", plain, plain)
	}
	var parseErr *providererrors.JSONParseError
	if errors.As(plain, &parseErr) {
		t.Fatalf("plain = %v, want no JSONParseError", plain)
	}

	_, secure := ChunkParseFailure(body, issue)
	if !errors.As(secure, &parseErr) {
		t.Fatalf("secure = %T %v, want *JSONParseError", secure, secure)
	}

	_, notJSON := ChunkParseFailurePlain(`{"a":`, nil)
	if !errors.As(notJSON, &parseErr) {
		t.Fatalf("not JSON = %T %v, want *JSONParseError", notJSON, notJSON)
	}

	msgPlain, _ := ChunkParseFailurePlain(`{"id":5}`, issue)
	msgSecure, _ := ChunkParseFailure(`{"id":5}`, issue)
	if msgPlain != msgSecure {
		t.Fatalf("messages differ without a prototype key:\n%s\n%s", msgPlain, msgSecure)
	}
}
