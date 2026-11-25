package providerutils

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR98integMediaSchemaParserPlainProtoKeySchemaFailure
// (cr-r98-carry-to-integrator): assemblyai-transcription-model.ts 318-320
// reads the poll with `await response.json()` (plain JSON.parse) and then
// schema.parse, so a body with a __proto__ key that fails the schema is the
// validation error, not a JSONParseError.
func TestR98integMediaSchemaParserPlainProtoKeySchemaFailure(t *testing.T) {
	body := []byte(`{"__proto__":{},"id":5,"status":"completed"}`)
	err := MediaSchemaParserPlain(SchemaAssemblyAITranscript)(200, nil, body)
	if err == nil {
		t.Fatal("want a schema error")
	}
	var parseErr *providererrors.JSONParseError
	if errors.As(err, &parseErr) {
		t.Fatalf("err = %v (JSONParseError), want the validation error", err)
	}
	var validation *providererrors.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("err = %T %v, want *providererrors.ValidationError", err, err)
	}
	// The secure variant still reports the forbidden prototype.
	if err := MediaSchemaParser(SchemaAssemblyAITranscript)(200, nil, body); !errors.As(err, &parseErr) {
		t.Fatalf("secure err = %T %v, want *providererrors.JSONParseError", err, err)
	}
}
