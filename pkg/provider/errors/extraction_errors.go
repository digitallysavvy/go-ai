package errors

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// ExtractionUnsupportedFieldTypeError indicates an extraction model does not
// support the type of a requested field. Mirrors TypeScript's
// ExtractionUnsupportedFieldTypeError (AI_ExtractionUnsupportedFieldTypeError).
type ExtractionUnsupportedFieldTypeError struct {
	FieldID   string
	FieldType string
	Provider  string
	ModelID   string
	Message   string
}

func (e *ExtractionUnsupportedFieldTypeError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf(
		"Field \"%s\" has type \"%s\", which is not supported by provider \"%s\" and model \"%s\".",
		e.FieldID, e.FieldType, e.Provider, e.ModelID,
	)
}

// IsExtractionUnsupportedFieldTypeError checks if an error is an
// ExtractionUnsupportedFieldTypeError.
func IsExtractionUnsupportedFieldTypeError(err error) bool {
	var target *ExtractionUnsupportedFieldTypeError
	return errors.As(err, &target)
}

// ExtractionRefusalError indicates an extraction model declined to extract
// one or more fields. Mirrors TypeScript's ExtractionRefusalError
// (AI_ExtractionRefusalError).
type ExtractionRefusalError struct {
	// FieldIDs lists the refused fields, sorted.
	FieldIDs []string
	Provider string
	ModelID  string
	Message  string
}

func (e *ExtractionRefusalError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	noun := "fields"
	if len(e.FieldIDs) == 1 {
		noun = "field"
	}
	quoted := make([]string, len(e.FieldIDs))
	for i, id := range e.FieldIDs {
		// JSON.stringify(id) in the TypeScript SDK.
		b, err := jsonstr.Marshal(id)
		if err != nil {
			b = []byte(strconv.Quote(id))
		}
		quoted[i] = string(b)
	}
	return fmt.Sprintf(
		"Extraction model \"%s\" from provider \"%s\" refused %s %s.",
		e.ModelID, e.Provider, noun, strings.Join(quoted, ", "),
	)
}

// IsExtractionRefusalError checks if an error is an ExtractionRefusalError.
func IsExtractionRefusalError(err error) bool {
	var target *ExtractionRefusalError
	return errors.As(err, &target)
}
