package errors

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
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

// DecisionRefusalError indicates a decision model declined to answer one or
// more questions. Mirrors TypeScript's DecisionRefusalError
// (AI_DecisionRefusalError).
type DecisionRefusalError struct {
	// QuestionIDs lists the refused questions, sorted.
	QuestionIDs []string
	Provider    string
	ModelID     string
	Message     string
}

func (e *DecisionRefusalError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	noun := "questions"
	if len(e.QuestionIDs) == 1 {
		noun = "question"
	}
	quoted := make([]string, len(e.QuestionIDs))
	for i, id := range e.QuestionIDs {
		// JSON.stringify(id) in the TypeScript SDK.
		b, err := json.Marshal(id)
		if err != nil {
			b = []byte(strconv.Quote(id))
		}
		quoted[i] = string(b)
	}
	return fmt.Sprintf(
		"Decision model \"%s\" from provider \"%s\" refused %s %s.",
		e.ModelID, e.Provider, noun, strings.Join(quoted, ", "),
	)
}

// IsDecisionRefusalError checks if an error is a DecisionRefusalError.
func IsDecisionRefusalError(err error) bool {
	var target *DecisionRefusalError
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
