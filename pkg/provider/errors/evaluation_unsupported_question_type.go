package errors

import (
	"errors"
	"fmt"
)

// EvaluationUnsupportedQuestionTypeError indicates an evaluation model does
// not support the type of a requested question. Mirrors TypeScript's
// EvaluationUnsupportedQuestionTypeError (AI_EvaluationUnsupportedQuestionTypeError).
type EvaluationUnsupportedQuestionTypeError struct {
	QuestionID   string
	QuestionType string
	Provider     string
	ModelID      string
	Message      string
}

func (e *EvaluationUnsupportedQuestionTypeError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf(
		"Question %q has type %q, which is not supported by provider %q and model %q.",
		e.QuestionID, e.QuestionType, e.Provider, e.ModelID,
	)
}

// IsEvaluationUnsupportedQuestionTypeError checks if an error is an
// EvaluationUnsupportedQuestionTypeError.
func IsEvaluationUnsupportedQuestionTypeError(err error) bool {
	var target *EvaluationUnsupportedQuestionTypeError
	return errors.As(err, &target)
}
