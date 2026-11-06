package ai

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestValidateObjectGenerationInputReportsInvalidArgumentError(t *testing.T) {
	cases := []struct {
		mode    ObjectOutputMode
		schema  bool
		name    string
		enum    []string
		field   string
		message string
	}{
		{ObjectModeObject, false, "", nil, "schema", "Schema is required for object output."},
		{ObjectModeArray, false, "", nil, "schema", "Element schema is required for array output."},
		{ObjectModeEnum, true, "", []string{"a"}, "schema", "Schema is not supported for enum output."},
		{ObjectModeEnum, false, "", nil, "enumValues", "Enum values are required for enum output."},
		{ObjectModeNoSchema, false, "n", nil, "schemaName", "Schema name is not supported for no-schema output."},
		{ObjectOutputMode("bogus"), false, "", nil, "output", "Invalid output type."},
	}
	for _, c := range cases {
		err := validateObjectGenerationInput(c.mode, c.schema, c.name, "", c.enum)
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) || ia.Field != c.field || ia.Message != c.message {
			t.Errorf("%v: err=%v", c.mode, err)
		}
	}
	if err := validateObjectGenerationInput(ObjectModeEnum, false, "", "", []string{"a"}); err != nil {
		t.Fatal(err)
	}
}
