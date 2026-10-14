package errors

import (
	"errors"
	"fmt"
)

// DecisionUnsupportedQuestionTypeError indicates a decision model does
// not support the type of a requested question. Mirrors TypeScript's
// DecisionUnsupportedQuestionTypeError (AI_DecisionUnsupportedQuestionTypeError).
type DecisionUnsupportedQuestionTypeError struct {
	QuestionID   string
	QuestionType string
	Provider     string
	ModelID      string
	Message      string
}

func (e *DecisionUnsupportedQuestionTypeError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf(
		"Question %q has type %q, which is not supported by provider %q and model %q.",
		e.QuestionID, e.QuestionType, e.Provider, e.ModelID,
	)
}

// IsDecisionUnsupportedQuestionTypeError checks if an error is an
// DecisionUnsupportedQuestionTypeError.
func IsDecisionUnsupportedQuestionTypeError(err error) bool {
	var target *DecisionUnsupportedQuestionTypeError
	return errors.As(err, &target)
}

// EvaluationUnsupportedQuestionTypeError is an alias of
// DecisionUnsupportedQuestionTypeError.
//
// Deprecated: Use DecisionUnsupportedQuestionTypeError instead.
type EvaluationUnsupportedQuestionTypeError = DecisionUnsupportedQuestionTypeError

// IsEvaluationUnsupportedQuestionTypeError checks if an error is a
// DecisionUnsupportedQuestionTypeError.
//
// Deprecated: Use IsDecisionUnsupportedQuestionTypeError instead.
func IsEvaluationUnsupportedQuestionTypeError(err error) bool {
	return IsDecisionUnsupportedQuestionTypeError(err)
}
